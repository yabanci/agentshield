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

// mockOllama creates a test server mimicking the Ollama /api/generate endpoint.
func mockOllama(t *testing.T, primaryFails bool) *httptest.Server {
	t.Helper()
	var calls atomic.Int32
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			w.WriteHeader(http.StatusOK)
			return
		}
		n := calls.Add(1)

		var req struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		if primaryFails && req.Model == agent.ModelPrimary {
			http.Error(w, "model error", http.StatusInternalServerError)
			return
		}

		resp := map[string]any{
			"response": "answer from " + req.Model + " (call " + itoa(n) + ")",
			"done":     true,
		}
		json.NewEncoder(w).Encode(resp)
	}))
}

func itoa(n int32) string {
	return string(rune('0' + n%10))
}

func newAgentWithURL(t *testing.T, url string) *agent.Agent {
	t.Helper()
	a := agent.NewWithOllamaURL(url)
	return a
}

func TestAsk_PrimarySuccess(t *testing.T) {
	srv := mockOllama(t, false)
	defer srv.Close()

	a := newAgentWithURL(t, srv.URL)
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

	a := newAgentWithURL(t, srv.URL)
	resp, err := a.Ask(context.Background(), "hello")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Tier != agent.TierFallback {
		t.Errorf("expected tier=fallback, got %s", resp.Tier)
	}
}

func TestAsk_CacheTierAfterPrimaryKilled(t *testing.T) {
	srv := mockOllama(t, false)
	defer srv.Close()

	a := newAgentWithURL(t, srv.URL)
	ctx := context.Background()

	// Prime the cache
	first, err := a.Ask(ctx, "what is go?")
	if err != nil {
		t.Fatalf("prime: %v", err)
	}
	if first.Tier != agent.TierPrimary {
		t.Fatalf("expected primary on first call, got %s", first.Tier)
	}

	// Kill both models
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
	srv := mockOllama(t, true)
	defer srv.Close()

	a := newAgentWithURL(t, srv.URL)
	a.KillFallback()

	resp, err := a.Ask(context.Background(), "unique prompt no cache "+t.Name())
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

	a := newAgentWithURL(t, srv.URL)
	a.KillPrimary()

	if s := a.Status(); !s.PrimaryKilled {
		t.Error("primary should be marked killed")
	}

	a.RestorePrimary()
	if s := a.Status(); s.PrimaryKilled {
		t.Error("primary should be restored")
	}
}
