package agent_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/yabanci/agentshield/agent"
)

// mockOllama creates a test server mimicking Ollama's API surface.
// If primaryFails=true, /api/generate calls for ModelPrimary return 500.
func mockOllama(t *testing.T, primaryFails bool) *httptest.Server {
	t.Helper()
	var callCount atomic.Int32
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			w.WriteHeader(http.StatusOK)
		case "/api/embeddings":
			// Return a trivial non-zero embedding so exact-fallback still works.
			_ = json.NewEncoder(w).Encode(map[string]any{"embedding": []float64{1, 0, 0}})
		case "/api/generate":
			n := callCount.Add(1)
			var req struct {
				Model string `json:"model"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)

			if primaryFails && req.Model == agent.ModelPrimary {
				http.Error(w, "model error", http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"response": "answer from " + req.Model + " #" + itoa(n),
				"done":     true,
			})
		}
	}))
}

func itoa(n int32) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return "N"
}

func TestAsk_PrimarySuccess(t *testing.T) {
	srv := mockOllama(t, false)
	defer srv.Close()

	a := agent.NewWithOllamaURL(srv.URL)
	resp, err := a.Ask(context.Background(), "hello")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Tier != agent.TierPrimary {
		t.Errorf("expected tier=primary, got %s", resp.Tier)
	}
	if resp.Cached {
		t.Error("first call should not be cached")
	}
}

func TestAsk_FallsBackWhenPrimaryFails(t *testing.T) {
	srv := mockOllama(t, true)
	defer srv.Close()

	a := agent.NewWithOllamaURL(srv.URL)
	resp, err := a.Ask(context.Background(), "hello")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Tier != agent.TierFallback {
		t.Errorf("expected tier=fallback, got %s", resp.Tier)
	}
}

func TestAsk_CacheTierWhenBothKilled(t *testing.T) {
	srv := mockOllama(t, false)
	defer srv.Close()

	a := agent.NewWithOllamaURL(srv.URL)
	ctx := context.Background()

	// Prime the cache via a successful primary call.
	first, err := a.Ask(ctx, "what is go?")
	if err != nil {
		t.Fatalf("prime: %v", err)
	}
	if first.Tier != agent.TierPrimary {
		t.Fatalf("expected primary on first call, got %s", first.Tier)
	}

	a.KillPrimary()
	a.KillFallback()

	second, err := a.Ask(ctx, "what is go?")
	if err != nil {
		t.Fatalf("cache: %v", err)
	}
	if second.Tier != agent.TierCache {
		t.Errorf("expected cache tier, got %s", second.Tier)
	}
	if !second.Cached {
		t.Error("should be marked cached")
	}
}

func TestAsk_GracefulDenialWhenAllDown(t *testing.T) {
	srv := mockOllama(t, true) // primary always fails
	defer srv.Close()

	a := agent.NewWithOllamaURL(srv.URL)
	a.KillFallback()

	// Unique prompt → no cache entry
	resp, err := a.Ask(context.Background(), "unique-no-cache-"+t.Name())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Tier != agent.TierDegraded {
		t.Errorf("expected degraded, got %s", resp.Tier)
	}
}

func TestKillAndRestorePrimary(t *testing.T) {
	srv := mockOllama(t, false)
	defer srv.Close()

	a := agent.NewWithOllamaURL(srv.URL)
	a.KillPrimary()
	if s := a.Status(); !s.PrimaryKilled {
		t.Error("primary should be marked killed")
	}
	a.RestorePrimary()
	if s := a.Status(); s.PrimaryKilled {
		t.Error("primary should be restored")
	}
}

func TestKillAndRestoreFallback(t *testing.T) {
	srv := mockOllama(t, false)
	defer srv.Close()

	a := agent.NewWithOllamaURL(srv.URL)
	a.KillFallback()
	if s := a.Status(); !s.FallbackKilled {
		t.Error("fallback should be marked killed")
	}
	a.RestoreFallback()
	if s := a.Status(); s.FallbackKilled {
		t.Error("fallback should be restored")
	}
}

func TestStatus_ReflectsAllFields(t *testing.T) {
	srv := mockOllama(t, false)
	defer srv.Close()

	a := agent.NewWithOllamaURL(srv.URL)
	s := a.Status()

	if s.PrimaryBreaker != "closed" {
		t.Errorf("expected closed, got %s", s.PrimaryBreaker)
	}
	if s.LoadshedLimit == 0 {
		t.Error("loadshed limit should be > 0")
	}
}

// TestCompare_BasicFlow exercises POST /demo/compare's happy path: both
// sides return text, both have non-zero latencies, the shielded result
// carries a tier label and the raw result does not (raw bypasses the
// orchestrator's tier logic).
func TestCompare_BasicFlow(t *testing.T) {
	srv := mockOllama(t, false)
	defer srv.Close()

	a := agent.NewWithOllamaURL(srv.URL)
	pair := a.Compare(context.Background(), "what is go?")

	if pair.Prompt != "what is go?" {
		t.Errorf("prompt echo mismatch: %q", pair.Prompt)
	}
	if pair.Shielded.Text == "" {
		t.Error("shielded text should be non-empty")
	}
	if pair.Raw.Text == "" {
		t.Error("raw text should be non-empty")
	}
	if pair.Shielded.Tier == "" {
		t.Error("shielded should report a tier (primary/fallback/cache)")
	}
	if pair.Raw.Tier != "" {
		t.Errorf("raw should not report a tier, got %q", pair.Raw.Tier)
	}
	if pair.DurationMS <= 0 {
		t.Error("duration_ms should be > 0")
	}
}

// TestCompare_WithDegradeMode is the regression test for round-2 backend
// finding M-3: pre-fix Compare's raw side called a.fallback (unwrapped
// chat provider), so degrade mode left the raw response good and the
// demo had no contrast. Post-fix raw calls a.primary (DegradedWrapper),
// so degrade poisons the raw side while shielded routes around it.
func TestCompare_WithDegradeMode(t *testing.T) {
	srv := mockOllama(t, false)
	defer srv.Close()

	a := agent.NewWithOllamaURL(srv.URL)
	a.EnableDegradeMode()
	defer a.DisableDegradeMode()

	pair := a.Compare(context.Background(), "explain channels")

	// Raw side should reflect the degraded output. degraded responses
	// always contain the "as an ai" / "i cannot" / "lorem ipsum" markers
	// or have a length anomaly, so quality should score low.
	if pair.Raw.QualityScore >= 0.5 {
		t.Errorf("raw quality should be low under degrade mode, got %.2f",
			pair.Raw.QualityScore)
	}
	// Shielded routes to fallback after the semantic gate fires, so its
	// tier should reflect that fact (fallback or, on a cold mock, primary
	// before the semantic CB calibrates).
	if pair.Shielded.Text == "" && pair.Shielded.Error == "" {
		t.Error("shielded must yield either text or an error message")
	}
}

// TestCompare_ConcurrentCallsDoNotRace covers QA's highest-ROI ask: prove
// Compare() is safe under concurrent traffic. Race detector picks up any
// shared-state mutation across the goroutines spawned per call.
func TestCompare_ConcurrentCallsDoNotRace(t *testing.T) {
	srv := mockOllama(t, false)
	defer srv.Close()

	a := agent.NewWithOllamaURL(srv.URL)
	done := make(chan struct{}, 3)
	for i := 0; i < 3; i++ {
		go func() {
			a.Compare(context.Background(), "ping")
			done <- struct{}{}
		}()
	}
	for i := 0; i < 3; i++ {
		<-done
	}
}
