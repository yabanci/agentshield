// trace.go — per-request resilience trace.
package memory

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// TraceOutcome describes why a tier attempt ended the way it did.
type TraceOutcome string

const (
	OutcomeSuccess         TraceOutcome = "success"
	OutcomeTransportError  TraceOutcome = "transport_error"
	OutcomeTransportCBOpen TraceOutcome = "transport_cb_open"
	OutcomeSemanticCBOpen  TraceOutcome = "semantic_cb_open"
	OutcomeSemanticFailure TraceOutcome = "semantic_failure"
	OutcomeKilled          TraceOutcome = "killed"
	OutcomeCacheHit        TraceOutcome = "cache_hit"
	OutcomeGracefulDenial  TraceOutcome = "graceful_denial"
)

// TraceStep records one tier attempt within a request.
type TraceStep struct {
	Tier           Tier         `json:"tier"`
	LatencyMS      int64        `json:"latency_ms"`
	TransportCB    string       `json:"transport_cb"`
	SemanticCB     string       `json:"semantic_cb"`
	QualityScore   *float64     `json:"quality_score,omitempty"`
	QualitySignals []string     `json:"quality_signals,omitempty"`
	Outcome        TraceOutcome `json:"outcome"`
}

// Trace is the full record of a single request's resilience journey.
type Trace struct {
	ID        string      `json:"id"`
	Prompt    string      `json:"prompt"`
	TotalMS   int64       `json:"total_ms"`
	FinalTier Tier        `json:"final_tier"`
	Steps     []TraceStep `json:"steps"`
	CreatedAt time.Time   `json:"created_at"`

	mu      sync.Mutex
	startAt time.Time
}

func newTrace(prompt string) *Trace {
	return &Trace{
		ID:        generateTraceID(),
		Prompt:    prompt,
		Steps:     make([]TraceStep, 0, 4),
		CreatedAt: time.Now(),
		startAt:   time.Now(),
	}
}

func (t *Trace) AddStep(step TraceStep) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Steps = append(t.Steps, step)
}

func (t *Trace) Finalize(tier Tier) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.FinalTier = tier
	t.TotalMS = time.Since(t.startAt).Milliseconds()
}

// TraceStore holds traces in memory with TTL eviction.
// Call Stop() to terminate the background cleanup goroutine.
type TraceStore struct {
	mu     sync.RWMutex
	traces map[string]*Trace
	ttl    time.Duration
	done   chan struct{}
}

func NewTraceStore() *TraceStore {
	s := &TraceStore{
		traces: make(map[string]*Trace),
		ttl:    30 * time.Minute,
		done:   make(chan struct{}),
	}
	go s.cleanup()
	return s
}

// NewTestTraceStore creates a TraceStore without the background cleanup goroutine.
func NewTestTraceStore() *TraceStore {
	return &TraceStore{
		traces: make(map[string]*Trace),
		ttl:    30 * time.Minute,
		done:   make(chan struct{}),
	}
}

// Stop terminates the background cleanup goroutine.
func (s *TraceStore) Stop() {
	select {
	case <-s.done:
	default:
		close(s.done)
	}
}

func (s *TraceStore) New(prompt string) *Trace {
	tr := newTrace(prompt)
	s.mu.Lock()
	s.traces[tr.ID] = tr
	s.mu.Unlock()
	return tr
}

func (s *TraceStore) Get(id string) *Trace {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.traces[id]
}

func (s *TraceStore) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			cutoff := time.Now().Add(-s.ttl)
			s.mu.Lock()
			for id, tr := range s.traces {
				if tr.CreatedAt.Before(cutoff) {
					delete(s.traces, id)
				}
			}
			s.mu.Unlock()
		}
	}
}

func generateTraceID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "tr_" + hex.EncodeToString(b)
}
