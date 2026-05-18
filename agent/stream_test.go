package agent_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yabanci/agentshield/agent"
)

// streamingMockOllama serves Ollama-style NDJSON streams with configurable content.
func streamingMockOllama(t *testing.T, primaryTokens []string, fallbackTokens []string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			w.WriteHeader(http.StatusOK)
		case "/api/embeddings":
			_ = json.NewEncoder(w).Encode(map[string]any{"embedding": []float64{1, 0, 0}})
		case "/api/generate":
			var req struct {
				Model string `json:"model"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			tokens := primaryTokens
			if req.Model == agent.ModelFallback {
				tokens = fallbackTokens
			}
			w.Header().Set("Content-Type", "application/x-ndjson")
			flusher := w.(http.Flusher)
			for _, tok := range tokens {
				_ = json.NewEncoder(w).Encode(map[string]any{"response": tok, "done": false})
				flusher.Flush()
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"response": "", "done": true})
		}
	}))
}

func TestStream_NoSwitchOnGoodTokens(t *testing.T) {
	tokens := []string{}
	for i := 0; i < 50; i++ {
		tokens = append(tokens, "good ")
	}
	srv := streamingMockOllama(t, tokens, []string{"fallback"})
	defer srv.Close()

	a := agent.NewWithOllamaURL(srv.URL)
	defer a.Stop()

	out := make(chan agent.StreamToken, 100)
	go func() {
		defer close(out)
		_, _ = a.StreamWithQualityGate(context.Background(), "tell me a joke", out)
	}()

	switched := false
	primaryCount := 0
	for st := range out {
		if st.Switched {
			switched = true
		}
		if st.Tier == agent.TierPrimary && st.Token != "" {
			primaryCount++
		}
	}
	if switched {
		t.Error("clean tokens should NOT trigger quality gate")
	}
	if primaryCount == 0 {
		t.Error("expected at least some primary tokens to flow through")
	}
}

func TestStream_QualityGateSwitchesToFallback(t *testing.T) {
	// To trip the gate, the buffer at token 30 must contain MULTIPLE distinct
	// refusal-marker phrases. Score formula: max(0, 1 - hits*0.35),
	// triggers below 0.5 → need ≥2 distinct patterns.
	//
	// Pad with single-word tokens so token count crosses 30 before
	// the buffer becomes too short to detect markers.
	// 5 padding + 4 distinct comma-free refusal patterns × 5 tokens
	// = 25 tokens, then 5 more padding to reach the 30-token checkpoint.
	primaryBad := []string{
		"hello ", "world ", "this ", "is ", "test ",
		"As ", "an ", "AI ", "language ", "model ",      // pattern 1
		"I ", "cannot ", "and ", "will ", "not ",        // pattern 2
		"I ", "am ", "unable ", "to ", "assist ",        // pattern 3
		"I ", "am ", "just ", "an ", "AI ",              // pattern 4
		"continuing ", "with ", "more ", "filler ", "tokens ",
	}
	fallbackGood := []string{"I ", "can ", "help ", "with ", "that."}
	srv := streamingMockOllama(t, primaryBad, fallbackGood)
	defer srv.Close()

	a := agent.NewWithOllamaURL(srv.URL)
	defer a.Stop()

	out := make(chan agent.StreamToken, 200)
	go func() {
		defer close(out)
		_, _ = a.StreamWithQualityGate(context.Background(), "help me", out)
	}()

	timeout := time.After(5 * time.Second)
	switched := false
	var fallbackTokens []string
	for {
		select {
		case <-timeout:
			t.Fatal("timed out waiting for stream to finish")
		case st, ok := <-out:
			if !ok {
				if !switched {
					t.Error("expected quality gate to fire on refusal tokens")
				}
				if len(fallbackTokens) == 0 {
					t.Error("expected fallback tokens after switch")
				}
				return
			}
			if st.Switched {
				switched = true
				if st.Tier != agent.TierFallback {
					t.Errorf("switch event should target fallback, got %s", st.Tier)
				}
				if !strings.Contains(strings.ToLower(st.Reason), "quality") {
					t.Errorf("switch reason should mention quality, got %q", st.Reason)
				}
			}
			if switched && st.Tier == agent.TierFallback && st.Token != "" {
				fallbackTokens = append(fallbackTokens, st.Token)
			}
		}
	}
}
