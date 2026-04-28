// Package agent wraps Ollama LLM calls with flowguard resilience primitives.
//
// Degradation chain:
//   Primary model (circuit breaker + retry + hedge)
//     → Fallback model (circuit breaker)
//     → Semantic cache (embeddings + cosine similarity)
//     → Graceful denial
//
// Additional protection layers:
//   Bulkhead  — isolates interactive vs batch request concurrency
//   Loadshed  — adaptive AIMD limit; sheds excess traffic under overload
package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"github.com/yabanci/flowguard/bulkhead"
	"github.com/yabanci/flowguard/circuitbreaker"
	"github.com/yabanci/flowguard/hedge"
	"github.com/yabanci/flowguard/loadshed"
	"github.com/yabanci/flowguard/retry"
)

const (
	ModelPrimary  = "llama3.2"
	ModelFallback = "llama3.2:1b"
)

// Tier describes which degradation level answered the request.
type Tier string

const (
	TierPrimary  Tier = "primary"
	TierFallback Tier = "fallback"
	TierCache    Tier = "cache"
	TierDegraded Tier = "degraded"
)

// Response is the result of a resilient LLM call.
type Response struct {
	Text   string `json:"text"`
	Tier   Tier   `json:"tier"`
	Cached bool   `json:"cached"`
}

// Status is a live snapshot of the agent's resilience state.
type Status struct {
	PrimaryBreaker   string  `json:"primary_breaker"`
	FallbackBreaker  string  `json:"fallback_breaker"`
	PrimaryKilled    bool    `json:"primary_killed"`
	FallbackKilled   bool    `json:"fallback_killed"`
	CacheSize        int     `json:"cache_size"`
	TotalRequests    int64   `json:"total_requests"`
	ErrorRate        float64 `json:"error_rate"`
	LoadshedLimit    int     `json:"loadshed_limit"`
	LoadshedInflight int     `json:"loadshed_inflight"`
	InteractiveBusy  int     `json:"interactive_busy"`
	BatchBusy        int     `json:"batch_busy"`
	ActiveSessions   int     `json:"active_sessions"`
	ChaosRunning     bool    `json:"chaos_running"`
}

// Agent is the resilient LLM client.
type Agent struct {
	ollama         *ollamaClient
	primaryCB      *circuitbreaker.Breaker
	fallbackCB     *circuitbreaker.Breaker
	hedger         *hedge.Hedge
	interactiveBH  *bulkhead.Bulkhead
	batchBH        *bulkhead.Bulkhead
	shedder        *loadshed.Shedder
	cache          *semanticCache
	tools          *ToolRegistry
	sessions       *SessionStore
	chaosMu        atomic.Bool // true while chaos is running
	primaryKilled  atomic.Bool
	fallbackKilled atomic.Bool
	totalRequests  atomic.Int64
}

func newAgent(ollamaURL string) *Agent {
	ol := &ollamaClient{
		http:    newHTTPClient(),
		baseURL: ollamaURL,
	}
	a := &Agent{
		ollama: ol,
		primaryCB: circuitbreaker.NewAdaptive(
			20, 0.5, 5,
			circuitbreaker.WithOpenTimeout(15*time.Second),
			circuitbreaker.WithSuccessThreshold(2),
		),
		fallbackCB: circuitbreaker.New(
			circuitbreaker.WithFailureThreshold(3),
			circuitbreaker.WithOpenTimeout(30*time.Second),
		),
		// Hedge: if primary model hasn't responded in 1.5s, fire a duplicate.
		// Returns whichever completes first.
		hedger: hedge.New(1500*time.Millisecond, hedge.WithMaxHedges(1)),

		// Bulkheads: limit concurrent requests per priority class.
		interactiveBH: bulkhead.New(20, bulkhead.WithMaxWait(2*time.Second)),
		batchBH:       bulkhead.New(5, bulkhead.WithMaxWait(0)),

		// Loadshed: adaptive AIMD — starts at 50 concurrent, shrinks under load.
		shedder: loadshed.New(50, 5*time.Second),
	}
	a.cache = newSemanticCache(10*time.Minute, ol.embed)
	a.tools = newToolRegistry(a)
	a.sessions = newSessionStore()
	return a
}

// New creates an Agent. Uses OLLAMA_URL env var if set, otherwise localhost.
func New() *Agent {
	url := os.Getenv("OLLAMA_URL")
	if url == "" {
		url = ollamaBaseURL
	}
	return newAgent(url)
}

// NewWithOllamaURL creates an Agent pointed at a custom Ollama URL (for testing).
func NewWithOllamaURL(url string) *Agent {
	a := newAgent(url)
	a.cache = newSemanticCache(10*time.Minute, nil)
	return a
}

// StartChaos runs the automated chaos scenario asynchronously.
// Returns a channel of events and an error if chaos is already running.
func (a *Agent) StartChaos(ctx context.Context) (<-chan ChaosEvent, error) {
	if !a.chaosMu.CompareAndSwap(false, true) {
		return nil, fmt.Errorf("chaos scenario already running")
	}
	ch := make(chan ChaosEvent, 64)
	go func() {
		defer close(ch)
		defer a.chaosMu.Store(false)
		a.RunChaos(ctx, ch)
	}()
	return ch, nil
}

// GetSession returns session history by ID.
func (a *Agent) GetSession(id string) *Session { return a.sessions.Get(id) }

// ListSessions returns all active sessions.
func (a *Agent) ListSessions() []Session { return a.sessions.List() }

// DeleteSession removes a session.
func (a *Agent) DeleteSession(id string) { a.sessions.Delete(id) }

// ToolList returns metadata about registered tools.
func (a *Agent) ToolList() []map[string]string { return a.tools.List() }

// ExecTool executes a named tool directly — exposed for testing.
func (a *Agent) ExecTool(ctx context.Context, name string, args map[string]any) (string, error) {
	return a.tools.Execute(ctx, name, args)
}

// Ask routes the prompt through the full degradation chain.
// Wraps the entire call with load shedder and bulkhead.
func (a *Agent) Ask(ctx context.Context, prompt string) (Response, error) {
	return a.ask(ctx, prompt, false)
}

// AskBatch is like Ask but uses the batch bulkhead (lower priority).
func (a *Agent) AskBatch(ctx context.Context, prompt string) (Response, error) {
	return a.ask(ctx, prompt, true)
}

func (a *Agent) ask(ctx context.Context, prompt string, batch bool) (Response, error) {
	a.totalRequests.Add(1)

	// Layer 1: load shedder
	var resp Response
	err := a.shedder.Do(ctx, func(ctx context.Context) error {
		// Layer 2: bulkhead (interactive vs batch)
		bh := a.interactiveBH
		if batch {
			bh = a.batchBH
		}
		return bh.Do(ctx, func(ctx context.Context) error {
			var bhErr error
			resp, bhErr = a.degrade(ctx, prompt)
			return bhErr
		})
	})

	if errors.Is(err, loadshed.ErrShed) {
		loadshedTotal.Inc()
		return Response{
			Text: "Server is overloaded. Please try again in a moment.",
			Tier: TierDegraded,
		}, nil
	}
	if errors.Is(err, bulkhead.ErrFull) {
		bulkheadFullTotal.WithLabelValues(func() string {
			if batch {
				return "batch"
			}
			return "interactive"
		}()).Inc()
		return Response{
			Text: "Too many concurrent requests. Please try again shortly.",
			Tier: TierDegraded,
		}, nil
	}
	if err != nil {
		return resp, err
	}

	a.updateCBMetrics()
	return resp, nil
}

// degrade runs the 4-tier degradation chain.
func (a *Agent) degrade(ctx context.Context, prompt string) (Response, error) {
	start := time.Now()

	// Tier 1: primary model (hedged + circuit breaker + retry)
	if text, ok := a.tryPrimary(ctx, prompt); ok {
		dur := time.Since(start)
		requestsTotal.WithLabelValues("primary").Inc()
		requestDuration.WithLabelValues("primary").Observe(dur.Seconds())
		a.cache.set(ctx, prompt, text)
		return Response{Text: text, Tier: TierPrimary}, nil
	}

	// Tier 2: fallback model (circuit breaker)
	if text, ok := a.tryFallback(ctx, prompt); ok {
		dur := time.Since(start)
		requestsTotal.WithLabelValues("fallback").Inc()
		requestDuration.WithLabelValues("fallback").Observe(dur.Seconds())
		a.cache.set(ctx, prompt, text)
		return Response{Text: text, Tier: TierFallback}, nil
	}

	// Tier 3: semantic cache
	if cached, ok := a.cache.get(ctx, prompt); ok {
		requestsTotal.WithLabelValues("cache").Inc()
		return Response{Text: cached, Tier: TierCache, Cached: true}, nil
	}

	// Tier 4: graceful denial
	requestsTotal.WithLabelValues("degraded").Inc()
	return Response{
		Text: "All AI tiers are currently unavailable. Please try again shortly.",
		Tier: TierDegraded,
	}, nil
}

// tryPrimary uses hedge + circuit breaker + retry.
// Hedge fires a duplicate primary request after 1.5s if the first is slow.
func (a *Agent) tryPrimary(ctx context.Context, prompt string) (string, bool) {
	if a.primaryKilled.Load() {
		return "", false
	}

	var result string
	hedgeFireCount := 0

	err := a.primaryCB.Do(ctx, func(ctx context.Context) error {
		return a.hedger.Do(ctx, func(ctx context.Context) error {
			hedgeFireCount++
			if hedgeFireCount > 1 {
				hedgeFiresTotal.Inc()
			}
			r := retry.New(
				retry.WithMaxRetries(2),
				retry.WithExponentialBackoff(300*time.Millisecond),
			)
			return r.Do(ctx, func(ctx context.Context) error {
				text, err := a.ollama.generate(ctx, ModelPrimary, prompt)
				if err != nil {
					return err
				}
				result = text
				return nil
			})
		})
	})

	if err != nil {
		return "", false
	}
	return result, true
}

// tryFallback uses a classic circuit breaker (no hedge — fallback must be fast).
func (a *Agent) tryFallback(ctx context.Context, prompt string) (string, bool) {
	if a.fallbackKilled.Load() {
		return "", false
	}
	var result string
	err := a.fallbackCB.Do(ctx, func(ctx context.Context) error {
		text, err := a.ollama.generate(ctx, ModelFallback, prompt)
		if err != nil {
			return err
		}
		result = text
		return nil
	})
	if err != nil {
		return "", false
	}
	return result, true
}

// StreamPrimary streams tokens from the primary model into the channel.
// If the primary CB is open or the model is killed, falls back to fallback model.
// Caller must drain or close the returned channel.
func (a *Agent) StreamPrimary(ctx context.Context, prompt string, tokens chan<- string) (Tier, error) {
	if !a.primaryKilled.Load() && a.primaryCB.State().String() == "closed" {
		err := a.ollama.stream(ctx, ModelPrimary, prompt, tokens)
		if err == nil {
			return TierPrimary, nil
		}
	}
	// Fallback stream
	if !a.fallbackKilled.Load() {
		err := a.ollama.stream(ctx, ModelFallback, prompt, tokens)
		if err == nil {
			return TierFallback, nil
		}
	}
	return TierDegraded, fmt.Errorf("all streaming tiers unavailable")
}

// KillPrimary / RestorePrimary simulate primary model failures.
func (a *Agent) KillPrimary()   { a.primaryKilled.Store(true) }
func (a *Agent) RestorePrimary() { a.primaryKilled.Store(false) }

// KillFallback / RestoreFallback simulate fallback model failures.
func (a *Agent) KillFallback()   { a.fallbackKilled.Store(true) }
func (a *Agent) RestoreFallback() { a.fallbackKilled.Store(false) }

// Status returns a live snapshot of all resilience layers.
func (a *Agent) Status() Status {
	primaryState := a.primaryCB.State().String()
	if a.primaryKilled.Load() {
		primaryState = "killed"
	}
	fallbackState := a.fallbackCB.State().String()
	if a.fallbackKilled.Load() {
		fallbackState = "killed"
	}
	return Status{
		PrimaryBreaker:   primaryState,
		FallbackBreaker:  fallbackState,
		PrimaryKilled:    a.primaryKilled.Load(),
		FallbackKilled:   a.fallbackKilled.Load(),
		CacheSize:        a.cache.size(),
		TotalRequests:    a.totalRequests.Load(),
		ErrorRate:        a.primaryCB.ErrorRate(),
		LoadshedLimit:    a.shedder.CurrentLimit(),
		LoadshedInflight: a.shedder.Inflight(),
		InteractiveBusy:  a.interactiveBH.ActiveCount(),
		BatchBusy:        a.batchBH.ActiveCount(),
		ActiveSessions:   a.sessions.Count(),
		ChaosRunning:     a.chaosMu.Load(),
	}
}

// Ping checks if Ollama is reachable.
func (a *Agent) Ping(ctx context.Context) error {
	if err := a.ollama.ping(ctx); err != nil {
		return fmt.Errorf("ollama unreachable: %w", err)
	}
	return nil
}

func (a *Agent) updateCBMetrics() {
	cbStateGauge.WithLabelValues("primary").Set(cbStateValue(a.primaryCB.State().String()))
	cbStateGauge.WithLabelValues("fallback").Set(cbStateValue(a.fallbackCB.State().String()))
	cacheSizeGauge.Set(float64(a.cache.size()))
}
