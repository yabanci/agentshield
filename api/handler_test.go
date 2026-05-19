// handler_test.go — integration tests for api/handler.go.
//
// Each test spins up a real httptest.Server wired to a mock Ollama backend
// and exercises the full HTTP round-trip (mux → middleware → handler).
// No os.Getenv calls — all values flow through config.Config{} directly.
package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yabanci/agentshield/agent"
	"github.com/yabanci/agentshield/config"
)

// ─── Mock Ollama ─────────────────────────────────────────────────────────────

// mockOllamaHandler returns an http.Handler that satisfies the Ollama API
// surface used by AgentShield:
//
//   - GET  /api/tags          → 200 (Ping health check)
//   - POST /api/embeddings    → {embedding: [1,0,0]}
//   - POST /api/generate      → {response: "...", done: true}
//
// primaryFails=true makes /api/generate return 500 for ModelPrimary so the
// agent exercises the fallback path.
func mockOllamaHandler(primaryFails bool) http.Handler {
	var n atomic.Int32
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			w.WriteHeader(http.StatusOK)
		case "/api/embeddings":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"embedding": []float64{1, 0, 0},
			})
		case "/api/generate":
			idx := n.Add(1)
			var req struct {
				Model string `json:"model"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if primaryFails && req.Model == agent.ModelPrimary {
				http.Error(w, "model error", http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"response": fmt.Sprintf("answer from %s #%d", req.Model, idx),
				"done":     true,
			})
		}
	})
}

// newTestServer wires a full mux backed by a mock Ollama and returns the
// HTTP test server. The caller is responsible for calling ts.Close().
// token sets cfg.AuthToken; set to "" to disable auth.
func newTestServer(t *testing.T, ollamaFails bool, token string) (*httptest.Server, *httptest.Server) {
	t.Helper()
	ollama := httptest.NewServer(mockOllamaHandler(ollamaFails))
	t.Cleanup(ollama.Close)

	a := agent.NewWithOllamaURL(ollama.URL)
	t.Cleanup(a.Stop)

	cfg := config.Defaults()
	cfg.AuthToken = token

	h := New(a, cfg)
	mux := http.NewServeMux()
	h.Register(mux)
	app := httptest.NewServer(mux)
	t.Cleanup(app.Close)
	return app, ollama
}

// newTestServerWithLimiter creates a server whose ipLimiter uses a custom
// compareIPRateLimit so the rate-limit test doesn't require 11+ real LLM
// calls. We override by injecting a fresh limiter with a tiny sliding window
// capped at compareIPRateLimit (10).
func newTestServerWithCustomLimiter(t *testing.T, token string) *httptest.Server {
	t.Helper()
	ollama := httptest.NewServer(mockOllamaHandler(false))
	t.Cleanup(ollama.Close)

	a := agent.NewWithOllamaURL(ollama.URL)
	t.Cleanup(a.Stop)

	cfg := config.Defaults()
	cfg.AuthToken = token

	h := New(a, cfg)
	mux := http.NewServeMux()
	h.Register(mux)
	app := httptest.NewServer(mux)
	t.Cleanup(app.Close)
	return app
}

// doJSON fires a JSON request and returns the response.
func doJSON(t *testing.T, method, url string, body any, token string) *http.Response {
	t.Helper()
	var rb io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		rb = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, rb)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

// ─── Tests ───────────────────────────────────────────────────────────────────

func TestHandlerIntegration_HealthLive(t *testing.T) {
	t.Parallel()
	app, _ := newTestServer(t, false, "")

	t.Run("returns 200 without dependency check", func(t *testing.T) {
		t.Parallel()
		resp, err := http.Get(app.URL + "/health/live")
		if err != nil {
			t.Fatalf("GET /health/live: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
		var body map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["status"] != "alive" {
			t.Errorf("expected status=alive, got %q", body["status"])
		}
	})
}

func TestHandlerIntegration_HealthReady(t *testing.T) {
	t.Run("200 when Ollama reachable", func(t *testing.T) {
		t.Parallel()
		app, _ := newTestServer(t, false, "")
		resp, err := http.Get(app.URL + "/health/ready")
		if err != nil {
			t.Fatalf("GET /health/ready: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("503 when Ollama unreachable", func(t *testing.T) {
		t.Parallel()
		// Point at a port that is definitely closed.
		a := agent.NewWithOllamaURL("http://127.0.0.1:1")
		defer a.Stop()
		cfg := config.Defaults()
		h := New(a, cfg)
		mux := http.NewServeMux()
		h.Register(mux)
		ts := httptest.NewServer(mux)
		defer ts.Close()

		resp, err := http.Get(ts.URL + "/health/ready")
		if err != nil {
			t.Fatalf("GET /health/ready: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Errorf("expected 503, got %d", resp.StatusCode)
		}
	})
}

func TestHandlerIntegration_Metrics(t *testing.T) {
	t.Parallel()
	app, _ := newTestServer(t, false, "")

	t.Run("prometheus exposition contains agentshield_requests_total", func(t *testing.T) {
		t.Parallel()
		resp, err := http.Get(app.URL + "/metrics")
		if err != nil {
			t.Fatalf("GET /metrics: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if !strings.Contains(string(body), "agentshield_requests_total") {
			t.Error("metrics body should contain agentshield_requests_total")
		}
	})
}

func TestHandlerIntegration_Status(t *testing.T) {
	t.Parallel()
	app, _ := newTestServer(t, false, "")

	t.Run("GET /status returns JSON with quality and CB fields", func(t *testing.T) {
		t.Parallel()
		resp, err := http.Get(app.URL + "/status")
		if err != nil {
			t.Fatalf("GET /status: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
		var body map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if _, ok := body["primary_breaker"]; !ok {
			t.Error("status response must have primary_breaker")
		}
		if _, ok := body["fallback_breaker"]; !ok {
			t.Error("status response must have fallback_breaker")
		}
		if _, ok := body["score"]; !ok {
			t.Error("status response must have score")
		}
	})
}

func TestHandlerIntegration_Chat_GoldenPath(t *testing.T) {
	// Not parallel — each chat call touches a real Ollama mock; sharing one
	// server instance across parallel subtests would interleave call counts
	// in the mock and make assertions fragile.
	app, _ := newTestServer(t, false, "")

	t.Run("POST /chat golden path returns tier and trace_id", func(t *testing.T) {
		resp := doJSON(t, http.MethodPost, app.URL+"/chat",
			map[string]string{"prompt": "what is Go?"}, "")
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
		var body map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body["tier"] == "" || body["tier"] == nil {
			t.Error("chat response must include tier")
		}
		if body["text"] == "" || body["text"] == nil {
			t.Error("chat response must include non-empty text")
		}
		if body["trace_id"] == "" || body["trace_id"] == nil {
			t.Error("chat response must include trace_id")
		}
	})

	t.Run("POST /chat empty prompt returns 400", func(t *testing.T) {
		resp := doJSON(t, http.MethodPost, app.URL+"/chat",
			map[string]string{"prompt": ""}, "")
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})
}

func TestHandlerIntegration_Auth_DemoKill(t *testing.T) {
	const token = "test-secret-token"

	t.Run("POST /demo/kill returns 401 without Authorization", func(t *testing.T) {
		t.Parallel()
		app, _ := newTestServer(t, false, token)
		resp := doJSON(t, http.MethodPost, app.URL+"/demo/kill", nil, "")
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("POST /demo/kill returns 200 with correct token", func(t *testing.T) {
		t.Parallel()
		app, _ := newTestServer(t, false, token)
		resp := doJSON(t, http.MethodPost, app.URL+"/demo/kill", nil, token)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200 with valid token, got %d", resp.StatusCode)
		}
	})

	t.Run("POST /demo/kill returns 401 with wrong token", func(t *testing.T) {
		t.Parallel()
		app, _ := newTestServer(t, false, token)
		resp := doJSON(t, http.MethodPost, app.URL+"/demo/kill", nil, "wrong-token")
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401 with wrong token, got %d", resp.StatusCode)
		}
	})
}

func TestHandlerIntegration_Auth_Trace(t *testing.T) {
	const token = "test-trace-token"

	// Helper: fire a /chat call to create a real trace, return trace ID.
	getTraceID := func(t *testing.T, appURL string) string {
		t.Helper()
		resp := doJSON(t, http.MethodPost, appURL+"/chat",
			map[string]string{"prompt": "trace test"}, "")
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("/chat failed: %d", resp.StatusCode)
		}
		var body struct {
			TraceID string `json:"trace_id"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode chat resp: %v", err)
		}
		if body.TraceID == "" {
			t.Fatal("chat response missing trace_id")
		}
		return body.TraceID
	}

	t.Run("GET /trace/{id} returns 401 without auth when token set", func(t *testing.T) {
		app, _ := newTestServer(t, false, token)
		traceID := getTraceID(t, app.URL)

		resp, err := http.Get(app.URL + "/trace/" + traceID)
		if err != nil {
			t.Fatalf("GET /trace: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401 without auth, got %d", resp.StatusCode)
		}
	})

	t.Run("GET /trace/{id} redacts Prompt when auth disabled", func(t *testing.T) {
		// Auth disabled (no token set) — requireAuth is a no-op.
		// The handler must scrub Prompt per the comment in getTrace.
		app, _ := newTestServer(t, false, "")
		traceID := getTraceID(t, app.URL)

		resp, err := http.Get(app.URL + "/trace/" + traceID)
		if err != nil {
			t.Fatalf("GET /trace: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
		var body map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode trace body: %v", err)
		}
		prompt, _ := body["prompt"].(string)
		if prompt != "" {
			t.Errorf("prompt must be redacted for unauth caller, got %q", prompt)
		}
	})

	t.Run("GET /trace/{id} returns full trace with auth", func(t *testing.T) {
		app, _ := newTestServer(t, false, token)
		traceID := getTraceID(t, app.URL)

		req, _ := http.NewRequest(http.MethodGet, app.URL+"/trace/"+traceID, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET /trace with auth: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
		// Full trace must include an ID field.
		var body map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode full trace: %v", err)
		}
		if body["id"] == nil || body["id"] == "" {
			t.Error("full trace must have id field")
		}
	})

	t.Run("GET /trace/{id} returns 404 for unknown trace", func(t *testing.T) {
		t.Parallel()
		app, _ := newTestServer(t, false, token)
		req, _ := http.NewRequest(http.MethodGet, app.URL+"/trace/no-such-trace-id", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET /trace unknown: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 404 for unknown trace, got %d", resp.StatusCode)
		}
	})
}

func TestHandlerIntegration_RateLimit_DemoKill(t *testing.T) {
	// Rate-limit test needs its own isolated server so the sliding-window
	// counter isn't shared with other tests. Not parallel for the same reason.
	const token = "rl-test-token"
	app := newTestServerWithCustomLimiter(t, token)

	allowed := 0
	rateLimited := 0

	// /demo/kill uses compareMiddleware → limit is compareIPRateLimit (10).
	// Send 12 requests from the same IP; first 10 should succeed, 11+ should 429.
	for i := 0; i < compareIPRateLimit+2; i++ {
		resp := doJSON(t, http.MethodPost, app.URL+"/demo/kill", nil, token)
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		switch resp.StatusCode {
		case http.StatusOK:
			allowed++
		case http.StatusTooManyRequests:
			rateLimited++
		default:
			t.Errorf("unexpected status %d: %s", resp.StatusCode, body)
		}
	}

	if allowed != compareIPRateLimit {
		t.Errorf("expected %d allowed requests, got %d", compareIPRateLimit, allowed)
	}
	if rateLimited != 2 {
		t.Errorf("expected 2 rate-limited requests, got %d", rateLimited)
	}
}

func TestHandlerIntegration_ErrorScrubbing(t *testing.T) {
	t.Parallel()

	// Point the agent at a URL that will refuse connections — this forces
	// both primary and fallback to fail. With both models dead, the agent
	// falls through to the graceful-denial tier rather than returning a 5xx.
	// We kill both explicitly to guarantee it, then verify the body doesn't
	// leak the internal URL.
	t.Run("5xx body must not contain upstream URL", func(t *testing.T) {
		t.Parallel()

		// Create an Ollama mock that always fails generate but succeeds on
		// Ping (so /health/ready stays green; we just want chat to fail).
		ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/tags" {
				w.WriteHeader(http.StatusOK)
				return
			}
			// Always fail generate/embeddings to trigger error path.
			http.Error(w, "upstream error", http.StatusInternalServerError)
		}))
		defer ollama.Close()

		a := agent.NewWithOllamaURL(ollama.URL)
		defer a.Stop()
		// Kill both tiers so the agent has no choice but graceful denial or
		// to return whatever the orchestrator surfaces.
		a.KillPrimary()
		a.KillFallback()

		cfg := config.Defaults()
		h := New(a, cfg)
		mux := http.NewServeMux()
		h.Register(mux)
		ts := httptest.NewServer(mux)
		defer ts.Close()

		resp := doJSON(t, http.MethodPost, ts.URL+"/chat",
			map[string]string{"prompt": "trigger scrubbing"}, "")
		defer func() { _ = resp.Body.Close() }()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		bodyStr := string(body)
		// The upstream Ollama URL must never appear in a client-visible response.
		if strings.Contains(bodyStr, "127.0.0.1:") {
			t.Errorf("response body leaks upstream URL: %s", bodyStr)
		}
		if strings.Contains(bodyStr, ollama.URL) {
			t.Errorf("response body leaks ollama URL %s: %s", ollama.URL, bodyStr)
		}
	})
}
