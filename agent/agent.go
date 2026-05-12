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
	"strings"
	"sync/atomic"
	"time"

	"github.com/yabanci/flowguard/bulkhead"
	"github.com/yabanci/flowguard/circuitbreaker"
	"github.com/yabanci/flowguard/hedge"
	"github.com/yabanci/flowguard/loadshed"
	"github.com/yabanci/flowguard/retry"

	"github.com/yabanci/agentshield/cache"
	"github.com/yabanci/agentshield/config"
	"github.com/yabanci/agentshield/provider"
	"github.com/yabanci/agentshield/quality"
	"github.com/yabanci/agentshield/telemetry"
)

const (
	ModelPrimary  = "llama3.2"
	ModelFallback = "llama3.2:1b"
)

// Tier describes which degradation level answered the request.
// Canonical type lives in telemetry — alias here so existing call sites work.
type Tier = telemetry.Tier

const (
	TierPrimary  = telemetry.TierPrimary
	TierFallback = telemetry.TierFallback
	TierCache    = telemetry.TierCache
	TierDegraded = telemetry.TierDegraded
)

// Response is the result of a resilient LLM call.
type Response struct {
	Text    string `json:"text"`
	Tier    Tier   `json:"tier"`
	Cached  bool   `json:"cached"`
	TraceID string `json:"trace_id,omitempty"`
}

// Status is a live snapshot of the agent's resilience state.
type Status struct {
	PrimaryBreaker     string             `json:"primary_breaker"`
	FallbackBreaker    string             `json:"fallback_breaker"`
	PrimaryKilled      bool               `json:"primary_killed"`
	FallbackKilled     bool               `json:"fallback_killed"`
	CacheSize          int                `json:"cache_size"`
	TotalRequests      int64              `json:"total_requests"`
	ErrorRate          float64            `json:"error_rate"`
	LoadshedLimit      int                `json:"loadshed_limit"`
	LoadshedInflight   int                `json:"loadshed_inflight"`
	InteractiveBusy    int                `json:"interactive_busy"`
	BatchBusy          int                `json:"batch_busy"`
	ActiveSessions     int                `json:"active_sessions"`
	ChaosRunning       bool               `json:"chaos_running"`
	PrimarySemanticCB  quality.SBSnapshot         `json:"primary_semantic_cb"`
	FallbackSemanticCB quality.SBSnapshot         `json:"fallback_semantic_cb"`
	DegradeMode        bool               `json:"degrade_mode"`
	Costs              telemetry.CostStats          `json:"costs"`
	TierCounts         telemetry.TierRequestCounts  `json:"tier_counts"`
	Latency            telemetry.LatencySnapshot    `json:"latency"`
	Score              telemetry.ResilienceScore    `json:"score"`
}

// Agent is the resilient LLM client.
type Agent struct {
	lifeCtx        context.Context
	lifeCancel     context.CancelFunc
	primary         provider.LLMProvider
	fallback        provider.LLMProvider
	embedder        provider.Embedder
	degradedPrimary *provider.DegradedWrapper
	primaryCB      *circuitbreaker.Breaker
	fallbackCB     *circuitbreaker.Breaker
	primarySemCB   *quality.SemanticBreaker
	fallbackSemCB  *quality.SemanticBreaker
	qualityEval    *quality.QualityEvaluator
	hedger         *hedge.Hedge
	interactiveBH  *bulkhead.Bulkhead
	batchBH        *bulkhead.Bulkhead
	shedder        *loadshed.Shedder
	cache          *cache.SemanticCache
	tools          *ToolRegistry
	sessions       *SessionStore
	traces         *TraceStore
	webhook        *telemetry.WebhookDispatcher
	costs          *telemetry.CostTracker
	latency        *telemetry.LatencyTracker
	scoreHistory   *ScoreHistory
	chaosMu        atomic.Bool
	primaryKilled  atomic.Bool
	fallbackKilled atomic.Bool
	totalRequests  atomic.Int64
}

func newAgent(ollamaURL string) *Agent {
	ol := provider.NewOllama(config.ProviderConfig{
		Kind:    "ollama",
		BaseURL: ollamaURL,
		Timeout: 60 * time.Second,
	})
	// Wrap primary in DegradedWrapper so chaos demo can inject low-quality
	// responses without taking the backend down. Fallback is NOT wrapped —
	// chaos affects primary only.
	degraded := provider.NewDegradedWrapper(ol)
	lifeCtx, lifeCancel := context.WithCancel(context.Background())
	a := &Agent{
		lifeCtx:         lifeCtx,
		lifeCancel:      lifeCancel,
		primary:         degraded,
		fallback:        ol,
		embedder:        ol,
		degradedPrimary: degraded,
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
	a.webhook = telemetry.NewWebhookDispatcher()

	a.primarySemCB = quality.NewSemanticBreaker(quality.DefaultSBConfig).
		WithStateChangeCallback(func(prev, next quality.SBState, reason string, avg float64) {
			a.webhook.Fire(telemetry.WebhookEvent{
				Event:      fmt.Sprintf("semantic_cb_%s", next),
				Model:      ModelPrimary,
				PrevState:  string(prev),
				NewState:   string(next),
				Reason:     reason,
				AvgQuality: avg,
				Timestamp:  time.Now(),
			})
		})

	a.fallbackSemCB = quality.NewSemanticBreaker(quality.SemanticBreakerConfig{
		WindowSize:        6,
		MinSamples:        2,
		DegradedThreshold: 0.55,
		FailingThreshold:  0.35,
		OpenTimeout:       30 * time.Second,
		RecoverySamples:   2,
	}).WithStateChangeCallback(func(prev, next quality.SBState, reason string, avg float64) {
		a.webhook.Fire(telemetry.WebhookEvent{
			Event:      fmt.Sprintf("semantic_cb_%s", next),
			Model:      ModelFallback,
			PrevState:  string(prev),
			NewState:   string(next),
			Reason:     reason,
			AvgQuality: avg,
			Timestamp:  time.Now(),
		})
	})

	a.costs = telemetry.NewCostTracker()
	a.latency = telemetry.NewLatencyTracker()
	a.scoreHistory = newScoreHistory(60)
	a.qualityEval = quality.NewEvaluator(a.embedder.Embed)
	a.cache = cache.New(10*time.Minute, a.embedder.Embed)
	a.tools = newToolRegistry(a)
	a.sessions = newSessionStore()
	a.traces = newTraceStore()
	return a
}

// New creates an Agent by loading config from environment.
// Convenience wrapper for callers that don't yet hold a *config.Config.
// Panics on invalid config — startup-time failure is preferable to runtime surprises.
func New() *Agent {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		panic("agentshield config: " + err.Error())
	}
	return NewWithConfig(cfg)
}

// NewWithConfig creates an Agent from an explicit Config.
// Preferred form for production wiring.
func NewWithConfig(cfg *config.Config) *Agent {
	return newAgent(cfg.Provider.BaseURL)
}

// NewWithOllamaURL creates an Agent for testing — no background cleanup goroutines.
func NewWithOllamaURL(url string) *Agent {
	a := newAgent(url)
	a.cache = cache.New(10*time.Minute, nil)
	// Replace stores with test variants that don't start background goroutines.
	a.traces = newTestTraceStore()
	a.sessions = NewTestSessionStore()
	return a
}

// Stop terminates all background goroutines (cleanup tickers, in-flight chaos).
// Call when the Agent is no longer needed.
func (a *Agent) Stop() {
	a.lifeCancel()
	a.traces.Stop()
	a.sessions.Stop()
}

// LifecycleContext returns a context that is cancelled when the Agent is
// stopped. Background tasks should derive from this so they terminate
// on graceful shutdown.
func (a *Agent) LifecycleContext() context.Context {
	return a.lifeCtx
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
	tr := a.traces.New(prompt)

	var resp Response
	err := a.shedder.Do(ctx, func(ctx context.Context) error {
		bh := a.interactiveBH
		if batch {
			bh = a.batchBH
		}
		return bh.Do(ctx, func(ctx context.Context) error {
			var bhErr error
			resp, bhErr = a.degrade(ctx, prompt, tr)
			return bhErr
		})
	})

	if errors.Is(err, loadshed.ErrShed) {
		telemetry.LoadshedTotal.Inc()
		resp = Response{Text: "Server is overloaded. Please try again in a moment.", Tier: TierDegraded}
		tr.addStep(TraceStep{Tier: TierDegraded, Outcome: OutcomeGracefulDenial, LatencyMS: 0})
		a.costs.Record(TierDegraded, prompt, "") // keep TierCounts in sync with TotalRequests
	} else if errors.Is(err, bulkhead.ErrFull) {
		telemetry.BulkheadFullTotal.WithLabelValues(func() string {
			if batch {
				return "batch"
			}
			return "interactive"
		}()).Inc()
		resp = Response{Text: "Too many concurrent requests. Please try again shortly.", Tier: TierDegraded}
		tr.addStep(TraceStep{Tier: TierDegraded, Outcome: OutcomeGracefulDenial, LatencyMS: 0})
		a.costs.Record(TierDegraded, prompt, "") // keep TierCounts in sync with TotalRequests
	} else if err != nil {
		return resp, err
	}

	tr.finalize(resp.Tier)
	resp.TraceID = tr.ID
	a.updateCBMetrics()
	return resp, nil
}

// degrade runs the 4-tier degradation chain, recording each attempt in tr.
func (a *Agent) degrade(ctx context.Context, prompt string, tr *Trace) (Response, error) {
	start := time.Now()

	// Tier 1: primary model (hedged + transport CB + semantic CB + retry)
	if text, ok := a.tryPrimary(ctx, prompt, tr); ok {
		dur := time.Since(start)
		telemetry.RequestsTotal.WithLabelValues("primary").Inc()
		telemetry.RequestDuration.WithLabelValues("primary").Observe(dur.Seconds())
		a.latency.Record(TierPrimary, dur)
		a.cache.Set(ctx, prompt, text)
		a.costs.Record(TierPrimary, prompt, text)
		return Response{Text: text, Tier: TierPrimary}, nil
	}

	// Tier 2: fallback model (transport CB + semantic CB)
	if text, ok := a.tryFallback(ctx, prompt, tr); ok {
		dur := time.Since(start)
		telemetry.RequestsTotal.WithLabelValues("fallback").Inc()
		telemetry.RequestDuration.WithLabelValues("fallback").Observe(dur.Seconds())
		a.latency.Record(TierFallback, dur)
		a.cache.Set(ctx, prompt, text)
		a.costs.Record(TierFallback, prompt, text)
		return Response{Text: text, Tier: TierFallback}, nil
	}

	// Tier 3: semantic cache
	if cached, ok := a.cache.Get(ctx, prompt); ok {
		dur := time.Since(start)
		telemetry.RequestsTotal.WithLabelValues("cache").Inc()
		a.latency.Record(TierCache, dur)
		tr.addStep(TraceStep{Tier: TierCache, Outcome: OutcomeCacheHit,
			LatencyMS: dur.Milliseconds()})
		a.costs.Record(TierCache, prompt, cached)
		return Response{Text: cached, Tier: TierCache, Cached: true}, nil
	}

	// Tier 4: graceful denial
	telemetry.RequestsTotal.WithLabelValues("degraded").Inc()
	tr.addStep(TraceStep{Tier: TierDegraded, Outcome: OutcomeGracefulDenial,
		LatencyMS: time.Since(start).Milliseconds()})
	a.costs.Record(TierDegraded, prompt, "")
	return Response{
		Text: "All AI tiers are currently unavailable. Please try again shortly.",
		Tier: TierDegraded,
	}, nil
}

// tryPrimary uses hedge + transport CB + semantic CB + retry.
//
// Key design: quality evaluation is OUTSIDE the transport CB so that
// semantic failures never pollute the transport circuit breaker's state.
// The two breakers are fully independent.
func (a *Agent) tryPrimary(ctx context.Context, prompt string, tr *Trace) (string, bool) {
	stepStart := time.Now()
	step := TraceStep{
		Tier:        TierPrimary,
		TransportCB: a.primaryCB.State().String(),
		SemanticCB:  string(a.primarySemCB.State()),
	}
	defer func() {
		step.LatencyMS = time.Since(stepStart).Milliseconds()
		tr.addStep(step)
	}()

	if a.primaryKilled.Load() {
		step.Outcome = OutcomeKilled
		return "", false
	}
	if a.primarySemCB.ShouldBlock() {
		step.Outcome = OutcomeSemanticCBOpen
		return "", false
	}

	var result string
	hedgeFireCount := 0

	transportErr := a.primaryCB.Do(ctx, func(ctx context.Context) error {
		return a.hedger.Do(ctx, func(ctx context.Context) error {
			hedgeFireCount++
			if hedgeFireCount > 1 {
				telemetry.HedgeFiresTotal.Inc()
			}
			r := retry.New(
				retry.WithMaxRetries(2),
				retry.WithExponentialBackoff(300*time.Millisecond),
			)
			return r.Do(ctx, func(ctx context.Context) error {
				text, err := a.generate(ctx, ModelPrimary, prompt)
				if err != nil {
					return err
				}
				result = text
				return nil
			})
		})
	})

	if transportErr != nil {
		step.Outcome = OutcomeTransportError
		step.TransportCB = a.primaryCB.State().String()
		return "", false
	}

	qr := a.qualityEval.Evaluate(ctx, prompt, result)
	a.primarySemCB.Record(qr.Score, qr)
	telemetry.QualityGauge.WithLabelValues("primary").Set(qr.Score)

	step.QualityScore = &qr.Score
	step.SemanticCB = string(a.primarySemCB.State())
	if len(qr.Signals) > 0 {
		names := make([]string, len(qr.Signals))
		for i, s := range qr.Signals {
			names[i] = s.Name
		}
		step.QualitySignals = names
	}

	if qr.Score < quality.QualityAcceptable && len(qr.Signals) > 0 {
		step.Outcome = OutcomeSemanticFailure
		return "", false
	}
	step.Outcome = OutcomeSuccess
	return result, true
}

// tryFallback uses transport CB + semantic CB (no hedge — fallback must be fast).
func (a *Agent) tryFallback(ctx context.Context, prompt string, tr *Trace) (string, bool) {
	stepStart := time.Now()
	step := TraceStep{
		Tier:        TierFallback,
		TransportCB: a.fallbackCB.State().String(),
		SemanticCB:  string(a.fallbackSemCB.State()),
	}
	defer func() {
		step.LatencyMS = time.Since(stepStart).Milliseconds()
		tr.addStep(step)
	}()

	if a.fallbackKilled.Load() {
		step.Outcome = OutcomeKilled
		return "", false
	}
	if a.fallbackSemCB.ShouldBlock() {
		step.Outcome = OutcomeSemanticCBOpen
		return "", false
	}

	var result string
	err := a.fallbackCB.Do(ctx, func(ctx context.Context) error {
		r, err := a.fallback.Generate(ctx, provider.Request{Model: ModelFallback, Prompt: prompt})
		text := r.Text
		if err != nil {
			return err
		}
		qr := a.qualityEval.Evaluate(ctx, prompt, text)
		a.fallbackSemCB.Record(qr.Score, qr)
		telemetry.QualityGauge.WithLabelValues("fallback").Set(qr.Score)
		step.QualityScore = &qr.Score
		step.SemanticCB = string(a.fallbackSemCB.State())
		result = text
		return nil
	})
	if err != nil {
		step.Outcome = OutcomeTransportError
		return "", false
	}
	step.Outcome = OutcomeSuccess
	return result, true
}

// generate calls the primary provider. Degrade injection is handled inside
// the DegradedWrapper decorator (provider/degraded.go) — no branch here.
func (a *Agent) generate(ctx context.Context, model, prompt string) (string, error) {
	r, err := a.primary.Generate(ctx, provider.Request{Model: model, Prompt: prompt})
	if err != nil {
		return "", err
	}
	return r.Text, nil
}

// StreamToken is a single event in the quality-gated stream.
type StreamToken struct {
	Token    string `json:"token,omitempty"`
	Done     bool   `json:"done,omitempty"`
	Tier     Tier   `json:"tier"`
	Switched bool   `json:"switched,omitempty"` // quality gate triggered mid-stream
	Reason   string `json:"reason,omitempty"`
}

// StreamWithQualityGate streams tokens with an inline quality gate.
// If hallucination markers are detected in the first 120 tokens,
// the stream aborts and automatically continues from the fallback model.
// The caller receives a StreamToken{Switched: true} event at the switch point.
func (a *Agent) StreamWithQualityGate(ctx context.Context, prompt string, out chan<- StreamToken) (Tier, error) {
	canUsePrimary := !a.primaryKilled.Load() &&
		!a.primarySemCB.ShouldBlock() &&
		a.primaryCB.State().String() == "closed"

	if canUsePrimary {
		// Use a cancellable child context so we can abort the primary stream
		// when the quality gate trips without leaking the stream goroutine.
		streamCtx, cancelStream := context.WithCancel(ctx)
		rawTokens := make(chan string, 64)
		// errCh creates a happens-before edge between the streamer goroutine
		// finishing and the consumer reading streamErr. Without this, the race
		// detector flags a read/write race on a plain `streamErr` variable.
		errCh := make(chan error, 1)

		// provider.LLMProvider.Stream owns closing rawTokens (per its contract).
		go func() {
			errCh <- a.primary.Stream(streamCtx, provider.Request{Model: ModelPrimary, Prompt: prompt}, rawTokens)
		}()

		var buf strings.Builder
		tokenCount := 0
		tripped := false

		for token := range rawTokens {
			buf.WriteString(token)
			tokenCount++

			// Quality gate: check every 30 tokens for hallucination markers.
			if tokenCount%30 == 0 && tokenCount <= 120 {
				hallScore, reason := quality.HallucinationScore(buf.String())
				if hallScore < 0.5 {
					tripped = true
					cancelStream() // abort primary stream
					for range rawTokens {
					} // drain so goroutine can exit
					out <- StreamToken{Switched: true, Tier: TierFallback,
						Reason: "quality gate: " + reason}
					break
				}
			}
			out <- StreamToken{Token: token, Tier: TierPrimary}
		}
		cancelStream() // no-op if already cancelled; always call to free resources

		streamErr := <-errCh
		if !tripped && streamErr == nil {
			return TierPrimary, nil
		}
	}

	// Fallback stream (no quality gate — fallback must always complete).
	// Provider closes rawTokens per LLMProvider contract.
	rawTokens := make(chan string, 64)
	go func() {
		_ = a.fallback.Stream(ctx, provider.Request{Model: ModelFallback, Prompt: prompt}, rawTokens)
	}()
	for token := range rawTokens {
		out <- StreamToken{Token: token, Tier: TierFallback}
	}
	return TierFallback, nil
}

// KillPrimary / RestorePrimary simulate primary model failures.
func (a *Agent) KillPrimary()   { a.primaryKilled.Store(true) }
func (a *Agent) RestorePrimary() { a.primaryKilled.Store(false) }

// KillFallback / RestoreFallback simulate fallback model failures.
func (a *Agent) KillFallback()    { a.fallbackKilled.Store(true) }
func (a *Agent) RestoreFallback() { a.fallbackKilled.Store(false) }

// EnableDegradeMode makes primary return low-quality responses (demo).
// Implemented as a decorator toggle — chaos is no longer a branch in the hot path.
func (a *Agent) EnableDegradeMode() { a.degradedPrimary.Enable() }

// DisableDegradeMode restores normal primary responses.
func (a *Agent) DisableDegradeMode() { a.degradedPrimary.Disable() }

// PrimarySemanticSnapshot returns the primary model's semantic CB snapshot.
func (a *Agent) PrimarySemanticSnapshot() quality.SBSnapshot { return a.primarySemCB.Snapshot() }

// FallbackSemanticSnapshot returns the fallback model's semantic CB snapshot.
func (a *Agent) FallbackSemanticSnapshot() quality.SBSnapshot { return a.fallbackSemCB.Snapshot() }

// GetTrace returns a trace by ID.
func (a *Agent) GetTrace(id string) *Trace { return a.traces.Get(id) }

// ScoreHistorySnapshot returns the recent score points for sparkline rendering.
func (a *Agent) ScoreHistorySnapshot() []ScorePoint { return a.scoreHistory.Snapshot() }

// SetWebhookURL configures the webhook endpoint.
func (a *Agent) SetWebhookURL(url string) { a.webhook.SetURL(url) }

// ClearWebhookURL removes the webhook.
func (a *Agent) ClearWebhookURL() { a.webhook.ClearURL() }

// WebhookURL returns the currently configured webhook URL.
func (a *Agent) WebhookURL() string { return a.webhook.URL() }

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
	pSem := a.primarySemCB.Snapshot()
	fSem := a.fallbackSemCB.Snapshot()
	telemetry.SemanticCBStateGauge.WithLabelValues("primary").Set(telemetry.SBStateValue(pSem.State))
	telemetry.SemanticCBStateGauge.WithLabelValues("fallback").Set(telemetry.SBStateValue(fSem.State))

	pr, fr, cr, dr := a.costs.TierCounts()
	tierCounts := telemetry.TierRequestCounts{Primary: pr, Fallback: fr, Cache: cr, Denied: dr}
	costs := a.costs.Snapshot()
	lat := a.latency.Snapshot()

	s := Status{
		PrimaryBreaker:     primaryState,
		FallbackBreaker:    fallbackState,
		PrimaryKilled:      a.primaryKilled.Load(),
		FallbackKilled:     a.fallbackKilled.Load(),
		CacheSize:          a.cache.Size(),
		TotalRequests:      a.totalRequests.Load(),
		ErrorRate:          a.primaryCB.ErrorRate(),
		LoadshedLimit:      a.shedder.CurrentLimit(),
		LoadshedInflight:   a.shedder.Inflight(),
		InteractiveBusy:    a.interactiveBH.ActiveCount(),
		BatchBusy:          a.batchBH.ActiveCount(),
		ActiveSessions:     a.sessions.Count(),
		ChaosRunning:       a.chaosMu.Load(),
		PrimarySemanticCB:  pSem,
		FallbackSemanticCB: fSem,
		DegradeMode:        a.degradedPrimary.IsEnabled(),
		Costs:              costs,
		TierCounts:         tierCounts,
		Latency:            lat,
	}
	s.Score = telemetry.ComputeScore(telemetry.ScoreInput{
		PrimaryBreaker:     s.PrimaryBreaker,
		FallbackBreaker:    s.FallbackBreaker,
		PrimaryKilled:      s.PrimaryKilled,
		FallbackKilled:     s.FallbackKilled,
		PrimarySemanticCB:  s.PrimarySemanticCB,
		FallbackSemanticCB: s.FallbackSemanticCB,
		DegradeMode:        s.DegradeMode,
		CacheSize:          s.CacheSize,
		TierCounts:         s.TierCounts,
		Costs:              s.Costs,
		Latency:            s.Latency,
	})
	a.scoreHistory.Record(s.Score.Total)
	return s
}

// Ping checks if Ollama is reachable.
func (a *Agent) Ping(ctx context.Context) error {
	if err := a.primary.Ping(ctx); err != nil {
		return fmt.Errorf("ollama unreachable: %w", err)
	}
	return nil
}

func (a *Agent) updateCBMetrics() {
	telemetry.CBStateGauge.WithLabelValues("primary").Set(telemetry.CBStateValue(a.primaryCB.State().String()))
	telemetry.CBStateGauge.WithLabelValues("fallback").Set(telemetry.CBStateValue(a.fallbackCB.State().String()))
	// cache size gauge is owned and updated by cache package internally.
}
