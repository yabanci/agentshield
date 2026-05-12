package orchestrator

import (
	"context"
	"log/slog"
	"time"

	"github.com/yabanci/flowguard/hedge"
	"github.com/yabanci/flowguard/retry"

	"github.com/yabanci/agentshield/cache"
	"github.com/yabanci/agentshield/internal/logkeys"
	"github.com/yabanci/agentshield/memory"
	"github.com/yabanci/agentshield/provider"
	"github.com/yabanci/agentshield/quality"
	"github.com/yabanci/agentshield/telemetry"
)

// Result is the outcome of a degradation-chain run. Agent translates this
// to the public agent.Response (adding TraceID for the API consumer).
type Result struct {
	Text   string
	Tier   memory.Tier
	Cached bool
}

// Orchestrator runs the 4-tier degradation chain and the streaming quality
// gate. It owns no transport state of its own (loadshed, bulkhead) — that
// stays in agent.Agent.ask() for now. Future Pipeline wrapper can pull
// those in here too.
type Orchestrator struct {
	log           *slog.Logger
	primary       provider.LLMProvider
	fallback      provider.LLMProvider
	primaryModel  string
	fallbackModel string
	breakers      *BreakerSet
	hedger        *hedge.Hedge
	eval          *quality.QualityEvaluator
	sc            *cache.SemanticCache
	tel           *telemetry.Store
	chaos         *Chaos
}

// Config bundles Orchestrator dependencies. Pass everything explicitly
// — no globals, no Agent backreference. Caller (Agent) wires it.
type Config struct {
	Log           *slog.Logger
	Primary       provider.LLMProvider
	Fallback      provider.LLMProvider
	PrimaryModel  string
	FallbackModel string
	Breakers      *BreakerSet
	Hedger        *hedge.Hedge
	Eval          *quality.QualityEvaluator
	Cache         *cache.SemanticCache
	Telemetry     *telemetry.Store
	Chaos         *Chaos
}

// New constructs an Orchestrator from its config.
// If cfg.Log is nil, slog.Default() is used.
func New(cfg Config) *Orchestrator {
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}
	log = log.With(slog.String(logkeys.Component, "orchestrator"))
	return &Orchestrator{
		log:           log,
		primary:       cfg.Primary,
		fallback:      cfg.Fallback,
		primaryModel:  cfg.PrimaryModel,
		fallbackModel: cfg.FallbackModel,
		breakers:      cfg.Breakers,
		hedger:        cfg.Hedger,
		eval:          cfg.Eval,
		sc:            cfg.Cache,
		tel:           cfg.Telemetry,
		chaos:         cfg.Chaos,
	}
}

// Degrade runs the 4-tier degradation chain, recording each attempt in tr.
// Returns the final Result. The third return is reserved for future use
// (currently always nil — failures are encoded as Tier=Degraded).
func (o *Orchestrator) Degrade(ctx context.Context, prompt string, tr *memory.Trace) Result {
	start := time.Now()
	reqLog := o.log.With(slog.String(logkeys.TraceID, tr.ID))

	// Tier 1: primary model (hedged + transport CB + semantic CB + retry)
	if text, ok := o.tryPrimary(ctx, prompt, tr); ok {
		dur := time.Since(start)
		telemetry.RequestsTotal.WithLabelValues("primary").Inc()
		telemetry.RequestDuration.WithLabelValues("primary").Observe(dur.Seconds())
		o.tel.Latency.Record(memory.TierPrimary, dur)
		o.sc.Set(ctx, prompt, text)
		o.tel.Costs.Record(memory.TierPrimary, prompt, text)
		reqLog.Info("tier served",
			slog.String(logkeys.Tier, string(memory.TierPrimary)),
			slog.String(logkeys.Outcome, "success"),
			slog.Int64(logkeys.LatencyMS, dur.Milliseconds()),
		)
		return Result{Text: text, Tier: memory.TierPrimary}
	}

	// Tier 2: fallback model (transport CB + semantic CB)
	if text, ok := o.tryFallback(ctx, prompt, tr); ok {
		dur := time.Since(start)
		telemetry.RequestsTotal.WithLabelValues("fallback").Inc()
		telemetry.RequestDuration.WithLabelValues("fallback").Observe(dur.Seconds())
		o.tel.Latency.Record(memory.TierFallback, dur)
		o.sc.Set(ctx, prompt, text)
		o.tel.Costs.Record(memory.TierFallback, prompt, text)
		reqLog.Warn("tier served via fallback",
			slog.String(logkeys.Tier, string(memory.TierFallback)),
			slog.String(logkeys.Outcome, "success"),
			slog.Int64(logkeys.LatencyMS, dur.Milliseconds()),
		)
		return Result{Text: text, Tier: memory.TierFallback}
	}

	// Tier 3: semantic cache
	if cached, ok := o.sc.Get(ctx, prompt); ok {
		dur := time.Since(start)
		telemetry.RequestsTotal.WithLabelValues("cache").Inc()
		o.tel.Latency.Record(memory.TierCache, dur)
		tr.AddStep(memory.TraceStep{
			Tier: memory.TierCache, Outcome: memory.OutcomeCacheHit,
			LatencyMS: dur.Milliseconds(),
		})
		o.tel.Costs.Record(memory.TierCache, prompt, cached)
		reqLog.Warn("tier served from semantic cache",
			slog.String(logkeys.Tier, string(memory.TierCache)),
			slog.String(logkeys.Outcome, "cache_hit"),
			slog.Int64(logkeys.LatencyMS, dur.Milliseconds()),
		)
		return Result{Text: cached, Tier: memory.TierCache, Cached: true}
	}

	// Tier 4: graceful denial
	telemetry.RequestsTotal.WithLabelValues("degraded").Inc()
	tr.AddStep(memory.TraceStep{
		Tier: memory.TierDegraded, Outcome: memory.OutcomeGracefulDenial,
		LatencyMS: time.Since(start).Milliseconds(),
	})
	o.tel.Costs.Record(memory.TierDegraded, prompt, "")
	reqLog.Error("all tiers exhausted — graceful denial",
		slog.String(logkeys.Tier, string(memory.TierDegraded)),
		slog.String(logkeys.Outcome, "graceful_denial"),
		slog.Int64(logkeys.LatencyMS, time.Since(start).Milliseconds()),
	)
	return Result{
		Text: "All AI tiers are currently unavailable. Please try again shortly.",
		Tier: memory.TierDegraded,
	}
}

// tryPrimary uses hedge + transport CB + semantic CB + retry.
//
// Key design: quality evaluation is OUTSIDE the transport CB so that
// semantic failures never pollute the transport circuit breaker's state.
// The two breakers are fully independent.
func (o *Orchestrator) tryPrimary(ctx context.Context, prompt string, tr *memory.Trace) (string, bool) {
	stepStart := time.Now()
	step := memory.TraceStep{
		Tier:        memory.TierPrimary,
		TransportCB: o.breakers.PrimaryTransport.State().String(),
		SemanticCB:  string(o.breakers.PrimarySemantic.State()),
	}
	defer func() {
		step.LatencyMS = time.Since(stepStart).Milliseconds()
		tr.AddStep(step)
	}()

	if o.chaos.IsPrimaryKilled() {
		step.Outcome = memory.OutcomeKilled
		return "", false
	}
	if o.breakers.PrimarySemantic.ShouldBlock() {
		step.Outcome = memory.OutcomeSemanticCBOpen
		return "", false
	}

	var result string
	hedgeFireCount := 0

	transportErr := o.breakers.PrimaryTransport.Do(ctx, func(ctx context.Context) error {
		return o.hedger.Do(ctx, func(ctx context.Context) error {
			hedgeFireCount++
			if hedgeFireCount > 1 {
				telemetry.HedgeFiresTotal.Inc()
			}
			r := retry.New(
				retry.WithMaxRetries(2),
				retry.WithExponentialBackoff(300*time.Millisecond),
			)
			return r.Do(ctx, func(ctx context.Context) error {
				text, err := o.generate(ctx, o.primaryModel, prompt)
				if err != nil {
					return err
				}
				result = text
				return nil
			})
		})
	})

	if transportErr != nil {
		step.Outcome = memory.OutcomeTransportError
		step.TransportCB = o.breakers.PrimaryTransport.State().String()
		return "", false
	}

	qr := o.eval.Evaluate(ctx, prompt, result)
	o.breakers.PrimarySemantic.Record(qr.Score, qr)
	telemetry.QualityGauge.WithLabelValues("primary").Set(qr.Score)

	step.QualityScore = &qr.Score
	step.SemanticCB = string(o.breakers.PrimarySemantic.State())
	if len(qr.Signals) > 0 {
		names := make([]string, len(qr.Signals))
		for i, s := range qr.Signals {
			names[i] = s.Name
		}
		step.QualitySignals = names
	}

	if qr.Score < quality.QualityAcceptable && len(qr.Signals) > 0 {
		step.Outcome = memory.OutcomeSemanticFailure
		return "", false
	}
	step.Outcome = memory.OutcomeSuccess
	return result, true
}

// tryFallback uses transport CB + semantic CB (no hedge — fallback must be fast).
func (o *Orchestrator) tryFallback(ctx context.Context, prompt string, tr *memory.Trace) (string, bool) {
	stepStart := time.Now()
	step := memory.TraceStep{
		Tier:        memory.TierFallback,
		TransportCB: o.breakers.FallbackTransport.State().String(),
		SemanticCB:  string(o.breakers.FallbackSemantic.State()),
	}
	defer func() {
		step.LatencyMS = time.Since(stepStart).Milliseconds()
		tr.AddStep(step)
	}()

	if o.chaos.IsFallbackKilled() {
		step.Outcome = memory.OutcomeKilled
		return "", false
	}
	if o.breakers.FallbackSemantic.ShouldBlock() {
		step.Outcome = memory.OutcomeSemanticCBOpen
		return "", false
	}

	var result string
	err := o.breakers.FallbackTransport.Do(ctx, func(ctx context.Context) error {
		r, err := o.fallback.Generate(ctx, provider.Request{Model: o.fallbackModel, Prompt: prompt})
		text := r.Text
		if err != nil {
			return err
		}
		qr := o.eval.Evaluate(ctx, prompt, text)
		o.breakers.FallbackSemantic.Record(qr.Score, qr)
		telemetry.QualityGauge.WithLabelValues("fallback").Set(qr.Score)
		step.QualityScore = &qr.Score
		step.SemanticCB = string(o.breakers.FallbackSemantic.State())
		result = text
		return nil
	})
	if err != nil {
		step.Outcome = memory.OutcomeTransportError
		return "", false
	}
	step.Outcome = memory.OutcomeSuccess
	return result, true
}

// generate calls the primary provider. Degrade injection is handled inside
// the DegradedWrapper decorator (provider/degraded.go) — no branch here.
func (o *Orchestrator) generate(ctx context.Context, model, prompt string) (string, error) {
	r, err := o.primary.Generate(ctx, provider.Request{Model: model, Prompt: prompt})
	if err != nil {
		return "", err
	}
	return r.Text, nil
}
