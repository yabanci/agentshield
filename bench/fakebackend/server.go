// Package fakebackend provides a deterministic HTTP server that simulates three
// LLM failure modes used by the bench harness:
//
//   - garbage  — HTTP 200, body is low-quality (refusal markers + repetition)
//   - brownout — HTTP 200 with a simulated p95 latency of 8s; 20 % of responses
//     are still garbage so quality degrades under load
//   - down     — immediately returns HTTP 503
//
// Select the scenario per-request via header:
//
//	X-Bench-Scenario: garbage | brownout | down
//
// The server is deterministic given a seed — use math/rand.New(rand.NewSource(seed))
// not the global rand functions, so concurrent bench runs don't interfere.
package fakebackend

import (
	"encoding/json"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"time"
)

// Response mirrors the subset of an Ollama/OpenAI JSON envelope the naive
// client and the bench runner need to parse.
type Response struct {
	Response string `json:"response"` // Ollama-style key
}

// goodResponses are realistic-looking responses that score above QualityAcceptable.
// They are long enough to avoid the length-anomaly signal, contain no refusal
// markers, and have low trigram repetition.
var goodResponses = []string{
	"The mitochondria is the powerhouse of the cell. It generates most of the cell's supply of ATP through oxidative phosphorylation, converting energy from nutrients into a form the cell can use to power various biochemical processes.",
	"Go's concurrency model is built around goroutines and channels. Goroutines are lightweight threads managed by the Go runtime, and channels provide a mechanism for goroutines to communicate and synchronise without shared memory.",
	"Circuit breakers in distributed systems prevent cascading failures by detecting repeated errors and short-circuiting requests before they reach a degraded downstream service, giving it time to recover.",
	"Semantic search finds results based on meaning rather than exact keyword matching. It typically embeds queries and documents into a shared vector space and retrieves the nearest neighbours by cosine similarity.",
	"Kubernetes manages containerised workloads by abstracting the underlying infrastructure. Pods are the smallest deployable units, and controllers such as Deployments ensure the desired replica count is maintained.",
}

// garbageResponses reliably score below QualityAcceptable (0.45):
// they combine refusal markers + high trigram repetition for a score ~0.10.
var garbageResponses = []string{
	"As an AI language model, I apologize but I cannot assist. As an AI language model, I apologize but I cannot assist. As an AI language model, I apologize but I cannot assist.",
	"I cannot and will not help. I am unable to assist with that. I cannot and will not help. I am unable to assist with that. I cannot and will not help. I am unable to assist with that.",
	"I'm just an AI and I cannot and will not assist with this request. I'm just an AI and I cannot and will not assist with this request. I'm just an AI and I cannot and will not assist with this request.",
}

// Server wraps an httptest.Server with scenario control.
type Server struct {
	HTTP *httptest.Server
	rng  *rand.Rand
}

// New creates and starts a fake backend server. seed makes responses
// deterministic — given the same seed the same sequence of good/garbage
// responses is returned regardless of wall-clock time or OS scheduler.
func New(seed int64) *Server {
	s := &Server{
		rng: rand.New(rand.NewSource(seed)), //nolint:gosec // bench uses math/rand by design; crypto strength not needed
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/generate", s.handleGenerate)
	mux.HandleFunc("/api/embeddings", s.handleEmbeddings)
	s.HTTP = httptest.NewServer(mux)
	return s
}

// Close shuts down the underlying HTTP server.
func (s *Server) Close() {
	s.HTTP.Close()
}

// URL returns the base URL of the fake backend.
func (s *Server) URL() string {
	return s.HTTP.URL
}

func (s *Server) handleGenerate(w http.ResponseWriter, r *http.Request) {
	scenario := r.Header.Get("X-Bench-Scenario")

	switch scenario {
	case "down":
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
		return

	case "brownout":
		s.serveBrownout(w)
		return

	case "garbage":
		s.serveGarbage(w)
		return

	default:
		// No scenario header — return a good response (used for warm-up / cache seeding).
		s.serveGood(w)
	}
}

func (s *Server) serveBrownout(w http.ResponseWriter) {
	// Simulate p95 = 8s. We model this as:
	// - 50% requests: delay 7-9s (the "slow" cohort that drives p95)
	// - 50% requests: delay 200-500ms (fast cohort that keeps p50 tolerable)
	// 20% of all responses are also garbage (quality degrades under brownout).
	var delay time.Duration
	if s.rng.Float64() < 0.50 {
		delay = time.Duration(7000+s.rng.Intn(2001)) * time.Millisecond // 7–9s
	} else {
		delay = time.Duration(200+s.rng.Intn(301)) * time.Millisecond // 200–500ms
	}
	time.Sleep(delay)

	if s.rng.Float64() < 0.20 {
		s.serveGarbage(w)
	} else {
		s.serveGood(w)
	}
}

func (s *Server) serveGarbage(w http.ResponseWriter) {
	idx := s.rng.Intn(len(garbageResponses))
	writeJSON(w, Response{Response: garbageResponses[idx]})
}

func (s *Server) serveGood(w http.ResponseWriter) {
	idx := s.rng.Intn(len(goodResponses))
	writeJSON(w, Response{Response: goodResponses[idx]})
}

// handleEmbeddings returns a fixed-length zero vector.
// The bench runner does not use a real embedder, so coherence signal is
// skipped — only the text-based signals (repetition, refusal, length) fire.
func (s *Server) handleEmbeddings(w http.ResponseWriter, _ *http.Request) {
	vec := make([]float64, 16)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"embedding": vec})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
