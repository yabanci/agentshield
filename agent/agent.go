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
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/yabanci/flowguard/bulkhead"
	"github.com/yabanci/flowguard/circuitbreaker"
	"github.com/yabanci/flowguard/hedge"
	"github.com/yabanci/flowguard/loadshed"

	"github.com/yabanci/agentshield/cache"
	"github.com/yabanci/agentshield/config"
	"github.com/yabanci/agentshield/internal/logkeys"
	"github.com/yabanci/agentshield/memory"
	"github.com/yabanci/agentshield/orchestrator"
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
	log            *slog.Logger
	cfg            *config.Config
	primaryModel   string
	fallbackModel  string
	primary   provider.LLMProvider
	fallback  provider.LLMProvider
	embedder  provider.Embedder
	breakers  *orchestrator.BreakerSet
	interactiveBH  *bulkhead.Bulkhead
	batchBH        *bulkhead.Bulkhead
	shedder        *loadshed.Shedder
	cache          *cache.SemanticCache
	tools          *ToolRegistry
	memory         *memory.Store
	telemetry      *telemetry.Store
	chaos          *orchestrator.Chaos
	orch           *orchestrator.Orchestrator
	totalRequests  atomic.Int64
}

func newAgent(cfg *config.Config, log *slog.Logger) *Agent {
	if log == nil {
		log = slog.Default()
	}
	log = log.With(slog.String(logkeys.Component, "agent"))

	primaryModel := cfg.Models.Primary
	if primaryModel == "" {
		primaryModel = ModelPrimary
	}
	fallbackModel := cfg.Models.Fallback
	if fallbackModel == "" {
		fallbackModel = ModelFallback
	}

	// Provider selection.
	//
	// Ollama is the default — local, free, ideal for demos and dev.
	// Setting LLM_PROVIDER=openai switches the chat backend to any
	// OpenAI-compatible /v1/chat/completions endpoint. The embedder stays
	// on Ollama by default (cost) unless OPENAI_EMBED_MODEL is set.
	var chatProvider provider.LLMProvider
	var embedProvider provider.Embedder
	switch cfg.Provider.Kind {
	case "openai":
		oa := provider.NewOpenAI(cfg.Provider)
		chatProvider = oa
		if cfg.Provider.EmbedModel != "" {
			embedProvider = oa
		} else {
			// Keep an Ollama embedder available so the cache and quality
			// evaluator still work; cost stays at zero unless caller opts in.
			// cfg.Models.Embedding flows through as the model name.
			embedProvider = provider.NewOllama(config.ProviderConfig{
				Kind:       "ollama",
				BaseURL:    "http://localhost:11434",
				EmbedModel: cfg.Models.Embedding,
				Timeout:    cfg.Provider.Timeout,
			})
		}
	default: // "ollama" or empty
		// Forward cfg.Models.Embedding so the operator can swap the embedder
		// model (e.g. mxbai-embed-large) without touching code.
		olCfg := cfg.Provider
		olCfg.EmbedModel = cfg.Models.Embedding
		ol := provider.NewOllama(olCfg)
		chatProvider = ol
		embedProvider = ol
	}

	// Wrap primary in DegradedWrapper so chaos demo can inject low-quality
	// responses without taking the backend down. Fallback is NOT wrapped —
	// chaos affects primary only.
	degraded := provider.NewDegradedWrapper(chatProvider)
	lifeCtx, lifeCancel := context.WithCancel(context.Background())
	a := &Agent{
		lifeCtx:       lifeCtx,
		lifeCancel:    lifeCancel,
		log:           log,
		cfg:           cfg,
		primaryModel:  primaryModel,
		fallbackModel: fallbackModel,
		primary:       degraded,
		fallback:      chatProvider,
		embedder:      embedProvider,
		chaos:         orchestrator.NewChaos(degraded),

		// Bulkheads: limit concurrent requests per priority class.
		// Interactive callers wait up to 2s for a slot; batch callers fail fast.
		interactiveBH: bulkhead.New(cfg.Limits.InteractiveSlots, bulkhead.WithMaxWait(2*time.Second)),
		batchBH:       bulkhead.New(cfg.Limits.BatchSlots, bulkhead.WithMaxWait(0)),

		// Loadshed: adaptive AIMD — shrinks limit on calls slower than
		// LoadshedWindow. For LLM workloads this should be tuned well above
		// typical model latency, otherwise normal calls trigger shrinkage.
		shedder: loadshed.New(cfg.Limits.LoadshedStart, cfg.Limits.LoadshedWindow),
	}
	a.telemetry = telemetry.NewStore()
	a.memory = memory.NewStore(cfg.Score.HistorySize)

	pt := circuitbreaker.NewAdaptive(
		cfg.Limits.PrimaryCBWindow, cfg.Limits.PrimaryCBErrorRate, 5,
		circuitbreaker.WithOpenTimeout(15*time.Second),
		circuitbreaker.WithSuccessThreshold(2),
	)
	ft := circuitbreaker.New(
		circuitbreaker.WithFailureThreshold(cfg.Limits.FallbackCBThreshold),
		circuitbreaker.WithOpenTimeout(30*time.Second),
	)
	// logCBTransition emits a structured event for every semantic CB state
	// change. SREs alerting off logs (not metrics) can pattern-match on
	// "semantic_cb_state_change" without scraping Prometheus. Severity
	// climbs with state: degraded → WARN, failing → ERROR.
	logCBTransition := func(model string, prev, next quality.SBState, reason string, avg float64) {
		args := []any{
			slog.String("event", "semantic_cb_state_change"),
			slog.String("model", model),
			slog.String("prev_state", string(prev)),
			slog.String("new_state", string(next)),
			slog.String("reason", reason),
			slog.Float64("avg_quality", avg),
		}
		switch next {
		case quality.SBFailing:
			log.Error("semantic circuit breaker opened (failing)", args...)
		case quality.SBDegraded:
			log.Warn("semantic circuit breaker degraded", args...)
		default:
			log.Info("semantic circuit breaker recovered", args...)
		}
	}

	ps := quality.NewSemanticBreaker(quality.DefaultSBConfig).
		WithStateChangeCallback(func(prev, next quality.SBState, reason string, avg float64) {
			logCBTransition(primaryModel, prev, next, reason, avg)
			a.telemetry.Webhook.Fire(telemetry.WebhookEvent{
				Event:      fmt.Sprintf("semantic_cb_%s", next),
				Model:      primaryModel,
				PrevState:  string(prev),
				NewState:   string(next),
				Reason:     reason,
				AvgQuality: avg,
				Timestamp:  time.Now(),
			})
		})
	fs := quality.NewSemanticBreaker(quality.SemanticBreakerConfig{
		WindowSize:        6,
		MinSamples:        2,
		DegradedThreshold: 0.55,
		FailingThreshold:  0.35,
		OpenTimeout:       30 * time.Second,
		RecoverySamples:   2,
	}).WithStateChangeCallback(func(prev, next quality.SBState, reason string, avg float64) {
		logCBTransition(fallbackModel, prev, next, reason, avg)
		a.telemetry.Webhook.Fire(telemetry.WebhookEvent{
			Event:      fmt.Sprintf("semantic_cb_%s", next),
			Model:      fallbackModel,
			PrevState:  string(prev),
			NewState:   string(next),
			Reason:     reason,
			AvgQuality: avg,
			Timestamp:  time.Now(),
		})
	})
	a.breakers = orchestrator.NewBreakerSet(pt, ft, ps, fs)

	eval := quality.NewEvaluator(a.embedder.Embed)
	a.cache = cache.New(
		cfg.Cache.TTL, a.embedder.Embed,
		cache.WithMaxEntries(cfg.Cache.MaxEntries),
		cache.WithThreshold(cfg.Cache.SimilarityThreshold),
	)
	a.tools = newToolRegistry(a)

	// Hedge: if primary hasn't responded within HedgeDelay, fire a duplicate.
	hedger := hedge.New(cfg.Limits.HedgeDelay, hedge.WithMaxHedges(1))

	a.orch = orchestrator.New(orchestrator.Config{
		Log:           log,
		Primary:       a.primary,
		Fallback:      a.fallback,
		PrimaryModel:  primaryModel,
		FallbackModel: fallbackModel,
		Breakers:      a.breakers,
		Hedger:        hedger,
		Eval:          eval,
		Cache:         a.cache,
		Telemetry:     a.telemetry,
		Chaos:         a.chaos,
		RetryMax:      cfg.Limits.RetryMax,
		RetryBackoff:  cfg.Limits.RetryBaseBackoff,
	})
	return a
}

// New creates an Agent by loading config from environment.
// Convenience wrapper for callers that don't yet hold a *config.Config.
// Panics on invalid config — startup-time failure is preferable to runtime surprises.
// Uses slog.Default() for logging; use NewWithConfig for explicit logger.
func New() *Agent {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		panic("agentshield config: " + err.Error())
	}
	return NewWithConfig(cfg, slog.Default())
}

// NewWithConfig creates an Agent from an explicit Config and logger.
// Preferred form for production wiring. If log is nil, slog.Default() is used.
func NewWithConfig(cfg *config.Config, log *slog.Logger) *Agent {
	return newAgent(cfg, log)
}

// NewWithOllamaURL creates an Agent for testing — no background cleanup goroutines.
// Builds a Defaults() config with the given Ollama URL.
func NewWithOllamaURL(url string) *Agent {
	cfg := config.Defaults()
	cfg.Provider.BaseURL = url
	a := newAgent(cfg, slog.Default())
	a.cache = cache.New(10*time.Minute, nil)
	// Replace stores with test variants that don't start background goroutines.
	a.memory.Traces = memory.NewTestTraceStore()
	a.memory.Sessions = memory.NewTestSessionStore()
	return a
}

// Stop terminates all background goroutines (cleanup tickers, in-flight chaos).
// Call when the Agent is no longer needed.
func (a *Agent) Stop() {
	a.lifeCancel()
	a.memory.Traces.Stop()
	a.memory.Sessions.Stop()
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
	if !a.chaos.TryStart() {
		return nil, fmt.Errorf("chaos scenario already running")
	}
	ch := make(chan ChaosEvent, 64)
	go func() {
		defer close(ch)
		defer a.chaos.Done()
		a.RunChaos(ctx, ch)
	}()
	return ch, nil
}

// GetSession returns session history by ID.
func (a *Agent) GetSession(id string) *memory.Session { return a.memory.Sessions.Get(id) }

// ListSessions returns all active sessions.
func (a *Agent) ListSessions() []memory.Session { return a.memory.Sessions.List() }

// DeleteSession removes a session.
func (a *Agent) DeleteSession(id string) { a.memory.Sessions.Delete(id) }

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
	tr := a.memory.Traces.New(prompt)

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
		tr.AddStep(memory.TraceStep{Tier: TierDegraded, Outcome: memory.OutcomeGracefulDenial, LatencyMS: 0})
		a.telemetry.Costs.Record(TierDegraded, prompt, "") // keep TierCounts in sync with TotalRequests
	} else if errors.Is(err, bulkhead.ErrFull) {
		telemetry.BulkheadFullTotal.WithLabelValues(func() string {
			if batch {
				return "batch"
			}
			return "interactive"
		}()).Inc()
		resp = Response{Text: "Too many concurrent requests. Please try again shortly.", Tier: TierDegraded}
		tr.AddStep(memory.TraceStep{Tier: TierDegraded, Outcome: memory.OutcomeGracefulDenial, LatencyMS: 0})
		a.telemetry.Costs.Record(TierDegraded, prompt, "") // keep TierCounts in sync with TotalRequests
	} else if err != nil {
		return resp, err
	}

	tr.Finalize(resp.Tier)
	resp.TraceID = tr.ID
	a.updateCBMetrics()
	return resp, nil
}

// degrade delegates to Orchestrator. Kept as a method on Agent so react.go
// can call it without taking an Orchestrator parameter.
func (a *Agent) degrade(ctx context.Context, prompt string, tr *memory.Trace) (Response, error) {
	r := a.orch.Degrade(ctx, prompt, tr)
	return Response{Text: r.Text, Tier: r.Tier, Cached: r.Cached}, nil
}

// StreamToken is re-exported from the orchestrator so existing handlers
// (api/handler.go) keep working without an import switch.
type StreamToken = orchestrator.StreamToken

// StreamWithQualityGate delegates to Orchestrator. Returns memory.Tier
// (which Tier aliases) so callers see no observable change.
func (a *Agent) StreamWithQualityGate(ctx context.Context, prompt string, out chan<- StreamToken) (Tier, error) {
	return a.orch.StreamWithQualityGate(ctx, prompt, out)
}

// CompareResult is the per-side outcome of a side-by-side comparison.
type CompareResult struct {
	Text         string  `json:"text"`
	Tier         string  `json:"tier,omitempty"`
	LatencyMS    int64   `json:"latency_ms"`
	QualityScore float64 `json:"quality_score"`
	Cached       bool    `json:"cached,omitempty"`
	TraceID      string  `json:"trace_id,omitempty"`
	Error        string  `json:"error,omitempty"`
}

// ComparePair bundles a shielded + raw run of the same prompt for the
// /demo/compare endpoint. Shielded goes through the full degradation
// chain; raw calls the underlying LLM provider directly with no CB, no
// retry, no quality gate, no semantic cache. The whole point is to make
// the value of AgentShield visible — during degrade mode, the raw side
// returns garbage with high latency and low quality_score; the shielded
// side returns a good answer via the fallback model.
type ComparePair struct {
	Prompt    string        `json:"prompt"`
	Shielded  CompareResult `json:"shielded"`
	Raw       CompareResult `json:"raw"`
	DurationMS int64        `json:"duration_ms"`
}

// Compare fires the same prompt through both the resilience stack and a
// raw provider call, concurrently. Returns once both finish (or fail).
// The raw side uses the primary model name but bypasses the chaos
// wrapper — i.e. it talks directly to the underlying LLM, so the user
// sees what would happen without AgentShield's mitigations.
func (a *Agent) Compare(ctx context.Context, prompt string) ComparePair {
	pair := ComparePair{Prompt: prompt}
	start := time.Now()

	type shieldedOut struct {
		resp Response
		err  error
		dur  time.Duration
	}
	type rawOut struct {
		text string
		err  error
		dur  time.Duration
	}
	shieldedCh := make(chan shieldedOut, 1)
	rawCh := make(chan rawOut, 1)

	go func() {
		t := time.Now()
		resp, err := a.Ask(ctx, prompt)
		shieldedCh <- shieldedOut{resp: resp, err: err, dur: time.Since(t)}
	}()

	go func() {
		t := time.Now()
		// Use a.primary (the DegradedWrapper around the chat provider) so
		// that the chaos demo's degrade-mode toggle still affects this
		// "raw" call — that's the whole point of the comparison. We do
		// NOT go through flowguard (no CB, no retry, no hedge, no semantic
		// gate), so this represents "what the LLM hands back if you call
		// it directly." When degrade is on, that's garbage; AgentShield's
		// shielded side then visibly out-performs.
		r, err := a.primary.Generate(ctx, provider.Request{
			Model:  a.primaryModel,
			Prompt: prompt,
		})
		rawCh <- rawOut{text: r.Text, err: err, dur: time.Since(t)}
	}()

	s := <-shieldedCh
	r := <-rawCh

	// Evaluate quality on both sides so the dashboard can show that the
	// raw response is, say, 0.18 while the shielded response is 0.91.
	eval := quality.NewEvaluator(a.embedder.Embed)

	pair.Shielded = CompareResult{
		Text:      s.resp.Text,
		Tier:      string(s.resp.Tier),
		Cached:    s.resp.Cached,
		TraceID:   s.resp.TraceID,
		LatencyMS: s.dur.Milliseconds(),
	}
	if s.err != nil {
		// Scrub the raw error string — it can contain internal URLs and
		// upstream provider URLs. Log full detail server-side via slog.
		a.log.Warn("compare shielded call failed", "err", s.err)
		pair.Shielded.Error = "shielded call failed"
	} else if s.resp.Text != "" {
		pair.Shielded.QualityScore = eval.Evaluate(ctx, prompt, s.resp.Text).Score
	}

	pair.Raw = CompareResult{
		Text:      r.text,
		LatencyMS: r.dur.Milliseconds(),
	}
	if r.err != nil {
		a.log.Warn("compare raw call failed", "err", r.err)
		pair.Raw.Error = "raw call failed"
	} else if r.text != "" {
		pair.Raw.QualityScore = eval.Evaluate(ctx, prompt, r.text).Score
	}

	pair.DurationMS = time.Since(start).Milliseconds()
	return pair
}

// KillPrimary / RestorePrimary simulate primary model failures.
func (a *Agent) KillPrimary() { a.chaos.KillPrimary() }
func (a *Agent) RestorePrimary() { a.chaos.RestorePrimary() }

// KillFallback / RestoreFallback simulate fallback model failures.
func (a *Agent) KillFallback()    { a.chaos.KillFallback() }
func (a *Agent) RestoreFallback() { a.chaos.RestoreFallback() }

// EnableDegradeMode makes primary return low-quality responses (demo).
// Implemented as a decorator toggle — chaos is no longer a branch in the hot path.
func (a *Agent) EnableDegradeMode() { a.chaos.EnableDegrade() }

// DisableDegradeMode restores normal primary responses.
func (a *Agent) DisableDegradeMode() { a.chaos.DisableDegrade() }

// PrimarySemanticSnapshot returns the primary model's semantic CB snapshot.
func (a *Agent) PrimarySemanticSnapshot() quality.SBSnapshot { return a.breakers.PrimarySemantic.Snapshot() }

// FallbackSemanticSnapshot returns the fallback model's semantic CB snapshot.
func (a *Agent) FallbackSemanticSnapshot() quality.SBSnapshot { return a.breakers.FallbackSemantic.Snapshot() }

// GetTrace returns a trace by ID.
func (a *Agent) GetTrace(id string) *memory.Trace { return a.memory.Traces.Get(id) }

// ScoreHistorySnapshot returns the recent score points for sparkline rendering.
func (a *Agent) ScoreHistorySnapshot() []memory.ScorePoint { return a.memory.ScoreHistory.Snapshot() }

// SetWebhookURL configures the webhook endpoint.
func (a *Agent) SetWebhookURL(url string) { a.telemetry.Webhook.SetURL(url) }

// ClearWebhookURL removes the webhook.
func (a *Agent) ClearWebhookURL() { a.telemetry.Webhook.ClearURL() }

// WebhookURL returns the currently configured webhook URL.
func (a *Agent) WebhookURL() string { return a.telemetry.Webhook.URL() }

// Status returns a live snapshot of all resilience layers.
func (a *Agent) Status() Status {
	primaryState := a.breakers.PrimaryTransport.State().String()
	if a.chaos.IsPrimaryKilled() {
		primaryState = "killed"
	}
	fallbackState := a.breakers.FallbackTransport.State().String()
	if a.chaos.IsFallbackKilled() {
		fallbackState = "killed"
	}
	pSem := a.breakers.PrimarySemantic.Snapshot()
	fSem := a.breakers.FallbackSemantic.Snapshot()
	telemetry.SemanticCBStateGauge.WithLabelValues("primary").Set(telemetry.SBStateValue(pSem.State))
	telemetry.SemanticCBStateGauge.WithLabelValues("fallback").Set(telemetry.SBStateValue(fSem.State))

	pr, fr, cr, dr := a.telemetry.Costs.TierCounts()
	tierCounts := telemetry.TierRequestCounts{Primary: pr, Fallback: fr, Cache: cr, Denied: dr}
	costs := a.telemetry.Costs.Snapshot()
	lat := a.telemetry.Latency.Snapshot()

	s := Status{
		PrimaryBreaker:     primaryState,
		FallbackBreaker:    fallbackState,
		PrimaryKilled:      a.chaos.IsPrimaryKilled(),
		FallbackKilled:     a.chaos.IsFallbackKilled(),
		CacheSize:          a.cache.Size(),
		TotalRequests:      a.totalRequests.Load(),
		ErrorRate:          a.breakers.PrimaryTransport.ErrorRate(),
		LoadshedLimit:      a.shedder.CurrentLimit(),
		LoadshedInflight:   a.shedder.Inflight(),
		InteractiveBusy:    a.interactiveBH.ActiveCount(),
		BatchBusy:          a.batchBH.ActiveCount(),
		ActiveSessions:     a.memory.Sessions.Count(),
		ChaosRunning:       a.chaos.IsRunning(),
		PrimarySemanticCB:  pSem,
		FallbackSemanticCB: fSem,
		DegradeMode:        a.chaos.IsDegradeEnabled(),
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
	a.memory.ScoreHistory.Record(s.Score.Total)
	return s
}

// Ping checks if the primary LLM provider is reachable. The error string
// uses the provider's own Name() so /health/ready output reflects whatever
// backend is actually configured (ollama / openai / groq / ...).
func (a *Agent) Ping(ctx context.Context) error {
	if err := a.primary.Ping(ctx); err != nil {
		return fmt.Errorf("%s provider unreachable: %w", a.primary.Name(), err)
	}
	return nil
}

func (a *Agent) updateCBMetrics() {
	telemetry.CBStateGauge.WithLabelValues("primary").Set(telemetry.CBStateValue(a.breakers.PrimaryTransport.State().String()))
	telemetry.CBStateGauge.WithLabelValues("fallback").Set(telemetry.CBStateValue(a.breakers.FallbackTransport.State().String()))
	// cache size gauge is owned and updated by cache package internally.
}
