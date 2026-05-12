# AgentShield Foundation Refactor — Design

**Date**: 2026-05-12
**Goal**: Reshape architecture to production-grade so that follow-on work (multi-provider, OTEL, summarization, tool-cache, OpenAPI, load-test) lays on a clean foundation. Resolves the known God-object debt before adding more capability.
**Hackathon deadline**: 2026-05-28 (~16 days available)
**Predecessor spec**: `2026-04-30-improvements-design.md` (Phases A+B+C — shipped)

---

## Problem statement

After Phases A+B+C the engineering quality is solid but the structure is straining:

- `agent.Agent` has **27 fields** spanning provider client, transport CBs, semantic CBs, quality eval, hedge/bulkhead/loadshed primitives, semantic cache, tools, sessions, traces, webhook, costs, latency, score history, and chaos state flags. New features get added by appending another field — there is nowhere else to put them.
- `dashboard.go` is ~900 lines of HTML/CSS/JS embedded in a Go string literal. No syntax highlight, no linter, no formatter, no IDE help.
- `os.Getenv` calls are scattered across `main.go`, `agent/`, and `api/`. Configuration is implicit and grows by accretion.
- `slog` is used in `main.go` only; `agent/` and `api/` have no structured logging at all. OTEL export is impossible without first introducing a logger.
- The Ollama HTTP client (`ollamaClient`) is used directly throughout `agent/`. Adding OpenAI / Groq / TrueFoundry inference requires shotgun edits in 8+ files.
- ReAct tools take `*Agent` directly. A misbehaving tool could in principle invoke `KillPrimary` or `EnableDegrade`. The blast radius of tool invocation is larger than necessary.

None of these block features. All of them make every future feature harder.

## Non-goals

Explicitly out of Foundation scope (each becomes its own brainstorm → spec → plan cycle):

- Multi-provider implementations (OpenAI, Groq, TrueFoundry inference) — F2 only ships the interface.
- OTEL exporter / W3C trace context propagation — F5 only ships structured logging; OTLP handler is a follow-up.
- Conversation summarization, tool-result caching, OpenAPI spec.
- Load testing (k6/vegeta), memory profiling, fuzz tests.
- Distributed mode (Redis-backed semantic cache, cluster-aware adaptive calibration).
- Demo video, Devpost text, README polish, TrueFoundry deployment.

## Success criteria

| Criterion | Verification |
|----------|--------------|
| `agent.Agent` ≤ 7 fields (5 collaborators + lifecycle ctx/cancel) | grep |
| Zero `os.Getenv` calls outside `config/` | `grep -rn "os.Getenv" --exclude-dir=config` returns empty |
| Zero HTML/CSS/JS literals in `.go` files | `grep -l "<html\|<style\|<script" agent/ api/` returns empty |
| Every component constructor accepts `*slog.Logger` | grep on `New*` signatures |
| `go test -race ./...` green after every MR | CI |
| `golangci-lint run` green incl. new `forbidigo` rules forbidding PII in log calls | CI |
| Coverage `config/`, `provider/` ≥ 95%; other new packages ≥ 80% | `go test -coverprofile` |
| Existing e2e scenarios (`agent_test.go`, `integration_test.go`) pass with no public-API change beyond `agent.New(cfg, logger)` | CI + manual run |
| `agent.New()` (no-arg) remains as a convenience that loads from env — backward compat for callers that don't yet hold a Config | manual smoke |

---

## Architecture (target)

```
agentshield/
├── agent/                       facade, ≤6 fields
│   └── agent.go                 New(cfg, provider, logger) / Ask / AskBatch / Stream / Status / Stop
├── orchestrator/                degradation chain + chaos + stream
│   ├── orchestrator.go          degrade() / tryPrimary() / tryFallback()
│   ├── pipeline.go              loadshed -> bulkhead(interactive|batch) -> orchestrator
│   ├── breakers.go              BreakerSet: transport+semantic per model
│   ├── stream.go                StreamWithQualityGate
│   └── chaos.go                 KillPrimary / Fallback / EnableDegrade — atomic flags
├── memory/                      session + trace + score history + tools (with Asker iface)
│   ├── sessions.go
│   ├── traces.go
│   ├── score_history.go
│   └── tools.go
├── telemetry/                   cost + latency + score + webhook + Prom metrics
│   ├── costs.go
│   ├── latency.go
│   ├── score.go                 ComputeScore (5×20)
│   ├── webhook.go
│   └── metrics.go               Prometheus collectors
├── quality/                     evaluator + semantic breaker
│   ├── evaluator.go
│   └── breaker.go
├── cache/
│   └── semantic.go
├── provider/                    LLMProvider interface + ollama impl + degraded decorator
│   ├── provider.go              interface LLMProvider, Embedder
│   ├── ollama.go
│   └── degraded.go              DegradedWrapper — chaos demo decorator
├── config/
│   ├── config.go                Config struct + LoadFromEnv + Validate
│   └── defaults.go              named constants for every magic number
└── api/                         no structural change — only Logger injection + dashboard moved out
    └── web/
        ├── templates/dashboard.html.tmpl
        ├── static/dashboard.css
        ├── static/dashboard.js
        └── embed.go             //go:embed
```

**Final `Agent` after F3b** (7 fields: 5 collaborators + 2 for lifecycle):

```go
type Agent struct {
    cfg        *config.Config
    log        *slog.Logger
    pipeline   *orchestrator.Pipeline   // owns Orchestrator + breakers + chaos
    memory     *memory.Store            // sessions + traces + tools + score history
    telemetry  *telemetry.Store         // costs + latency + score + webhook
    lifeCtx    context.Context
    lifeCancel context.CancelFunc
}
```

**Contracts that must not leak between packages:**
- `provider.LLMProvider` is the only way `orchestrator` talks to models. Zero `*ollamaClient` references outside `provider/ollama.go`.
- `quality` and `cache` accept `provider.Embedder` (narrow interface — Embed only). They never see `LLMProvider`.
- `orchestrator.Orchestrator` constructor takes: two `provider.LLMProvider`s (primary, fallback), `quality.Evaluator`, `quality.BreakerSet`, `cache.SemanticCache`, `telemetry.Recorder`, `*slog.Logger`. Pure DI, zero globals.
- `memory.Store`, `telemetry.Store`, `cache.SemanticCache` each expose `Stop()` invoked by `agent.Agent.Stop()`.

**Circular-dependency resolution:**

`memory.tools` currently holds `*Agent` for recursive ReAct calls. After Foundation it accepts `Asker` (least-authority interface):

```go
package memory

type Asker interface {
    Ask(ctx context.Context, prompt string) (string, error)
}
```

`agent.Agent` satisfies `Asker` by adapting its richer `Ask(ctx) (Response, error)` to the narrow string signature. Tools see only the response text — never the `Tier` (which would let them detect "I'm running on fallback") and never privileged operations like `KillPrimary` or `EnableDegrade`. This is deliberate sandboxing in the spirit of the project's resilience theme.

`agent.Response` (with `Text` / `Tier` / `Cached` / `TraceID`) stays in package `agent` — handlers consume it directly. Provider's `provider.Response` (with `Text` / `InputTokens` / `OutputTokens` / `FinishReason`) is a different type, internal to the provider→orchestrator boundary. Orchestrator translates provider response → agent response when assembling the final result.

---

## MR breakdown

Six sequential MRs. Each MR keeps `go test ./...` and CI green by itself; no MR depends on a later one to compile.

| MR | Scope | LoC ± | Risk | Unblocks |
|----|-------|------|------|----------|
| **F1** | `config` package + `LoadFromEnv` + `Validate`; agent constructor accepts `*config.Config` | +250 / -80 | low | everything (DI without env reads) |
| **F2** | `provider` package: `LLMProvider`, `Embedder`, `OllamaProvider` (extract `ollamaClient`), `DegradedWrapper` decorator | +200 / -120 | low | multi-provider follow-up |
| **F3a** | extract `memory`, `telemetry`, `quality`, `cache` packages (no hot-path change) | +400 / -350 | low-med | OTEL spans, summarization, tool-cache |
| **F3b** | extract `orchestrator` (with internal `pipeline`/`chaos`/`stream`); collapse 27→6 fields in `Agent` | +700 / -550 | **medium — biggest single refactor** | clean facade for everything else |
| **F4** | `dashboard.go` (~900 LoC literal) → `embed.FS` with `templates/` + `static/` | +900 / -900 | low | normal UI iteration (prettier, stylelint, IDE) |
| **F5** | `slog` through `agent/` + `api/`; `LoggerFromContext`; standardised log keys | +300 / -50 | low | OTEL exporter trivially attaches OTLP handler to slog |

### F1 — `config` package (detail)

```go
package config

type Config struct {
    Port      string
    AuthToken string
    Logger    LoggerConfig
    Provider  ProviderConfig
    Models    ModelsConfig
    Limits    LimitsConfig
    Quality   QualityConfig
    Cache     CacheConfig
    Webhook   WebhookConfig
    Score     ScoreConfig
}

type ProviderConfig struct {
    Kind    string        // "ollama"; later "openai"/"groq"/"tfinference"
    BaseURL string        // OLLAMA_URL
    Timeout time.Duration // default 60s
}

type ModelsConfig struct {
    Primary   string // default "llama3.2"
    Fallback  string // default "llama3.2:1b"
    Embedding string // default "nomic-embed-text"
}

type LimitsConfig struct {
    MaxPromptBytes      int           // default 32768
    ToolTimeout         time.Duration // default 10s
    InteractiveSlots    int           // default 20
    BatchSlots          int           // default 5
    LoadshedStart       int           // default 50
    LoadshedWindow      time.Duration // default 5s
    PrimaryCBWindow     int           // default 20
    PrimaryCBErrorRate  float64       // default 0.5
    FallbackCBThreshold int           // default 3
    HedgeDelay          time.Duration // default 1500ms
    RetryMax            int           // default 2
    RetryBaseBackoff    time.Duration // default 300ms
}

type QualityConfig struct {
    AcceptableScore float64           // default 0.5
    Coherence       CoherenceConfig
    HallucPatterns  []string
    DriftWindow     int               // default 50
    DriftSigma      float64           // default 2.0
}

type CacheConfig struct {
    TTL                  time.Duration // default 10m
    SimilarityThreshold  float64       // default 0.92
    MaxEntries           int           // default 1024
    EmbedAsync           bool          // default true
}

type WebhookConfig struct {
    AllowHTTP    bool          // AGENTSHIELD_ALLOW_HTTP_WEBHOOK
    AllowPrivate bool          // AGENTSHIELD_ALLOW_PRIVATE_WEBHOOK
    Timeout      time.Duration // default 5s
}

type ScoreConfig struct {
    HistorySize      int            // default 60
    LatencyP95Target time.Duration  // default 3s
    Weights          map[string]int // {transport:20, quality:20, cache:20, availability:20, latency:20}
}

func LoadFromEnv() (*Config, error)
func (c *Config) Validate() error
```

**Rules:**
- `Validate` runs inside `LoadFromEnv` — fail-fast at startup, never at request 1000.
- Zero `os.Getenv` outside `config/`. Enforced via `forbidigo` lint rule.
- `Validate` produces human-readable diagnostics for `time.Duration` parse errors (`"expected duration like '5s', got '5'"` rather than the stdlib `"missing unit in duration"`).
- `Score.Weights` is `map[string]int` so future score components don't require a struct field.

### F2 — `provider` package (detail)

```go
package provider

type Request struct {
    Model     string
    Prompt    string
    System    string
    MaxTokens int
    Stop      []string
}

type Response struct {
    Text         string
    InputTokens  int
    OutputTokens int
    FinishReason string
}

type LLMProvider interface {
    Generate(ctx context.Context, req Request) (Response, error)
    Stream(ctx context.Context, req Request, out chan<- string) error // provider closes `out`
    Embed(ctx context.Context, text string) ([]float32, error)
    Ping(ctx context.Context) error
    Name() string
}

type Embedder interface {
    Embed(ctx context.Context, text string) ([]float32, error)
}
```

```go
package provider

type OllamaProvider struct { /* http client + base URL + cfg + log */ }
func NewOllama(cfg config.ProviderConfig, log *slog.Logger) *OllamaProvider
// implements LLMProvider

type DegradedWrapper struct {
    inner   LLMProvider
    enabled atomic.Bool
}
func NewDegradedWrapper(inner LLMProvider) *DegradedWrapper
func (d *DegradedWrapper) Enable() / Disable() / IsEnabled() bool
// Generate returns degraded synthetic response if enabled, else delegates.
```

**Rules:**
- `Stream(ctx, req, out)` contract: **provider closes `out`** when stream ends (success or error). Documented in interface comment. Avoids double-close races at consumer side.
- `Request`/`Response` are provider-agnostic. No JSON tags from Ollama leak into the public API.
- `DegradedWrapper` is a decorator — chaos injection is no longer a branch in the hot path. Future chaos strategies (latency injection, partial output corruption) extend this pattern uniformly.

### F3a — extract memory + telemetry + quality + cache (detail)

Pure file moves with package rename. No behavioural change. Test files travel with their source.

| Old path | New path |
|----------|----------|
| `agent/session.go` | `memory/sessions.go` |
| `agent/trace.go` | `memory/traces.go` |
| `agent/score_history.go` | `memory/score_history.go` |
| `agent/tool.go` | `memory/tools.go` |
| `agent/cost.go` | `telemetry/costs.go` |
| `agent/latency.go` | `telemetry/latency.go` |
| `agent/score.go` | `telemetry/score.go` |
| `agent/webhook.go` | `telemetry/webhook.go` |
| `agent/metrics.go` | `telemetry/metrics.go` |
| `agent/quality.go` | `quality/evaluator.go` |
| `agent/semantic_breaker.go` | `quality/breaker.go` |
| `agent/cache.go` | `cache/semantic.go` |

**Aggregator types**:
```go
package memory
type Store struct { Sessions *SessionStore; Traces *TraceStore; Tools *ToolRegistry; ScoreHistory *ScoreHistory }
func New(cfg *config.Config, asker Asker, log *slog.Logger) *Store
func (s *Store) Stop()

package telemetry
type Store struct { Costs *CostTracker; Latency *LatencyTracker; Webhook *WebhookDispatcher; metrics — package-level }
func New(cfg *config.Config, log *slog.Logger) *Store
func (s *Store) Stop()
```

`agent.Agent` calls `memory.New` and `telemetry.New` instead of constructing each piece individually. After F3a `Agent` field count drops from 27 to ~12.

### F3b — extract orchestrator + pipeline (detail)

```go
package orchestrator

// Pipeline is the public entry: shed -> bulkhead -> degrade chain.
type Pipeline struct {
    cfg          *config.Config
    log          *slog.Logger
    shedder      *loadshed.Shedder
    interactive  *bulkhead.Bulkhead
    batch        *bulkhead.Bulkhead
    orch         *Orchestrator
    chaos        *Chaos
}
func NewPipeline(cfg *config.Config, primary, fallback provider.LLMProvider,
    eval *quality.Evaluator, breakers *quality.BreakerSet,
    sc *cache.SemanticCache, tel *telemetry.Store,
    log *slog.Logger) *Pipeline
func (p *Pipeline) Do(ctx context.Context, prompt string, batch bool) (Response, error)
func (p *Pipeline) Stream(ctx context.Context, prompt string, out chan<- StreamToken) (Tier, error)
func (p *Pipeline) Chaos() *Chaos // exposes Kill/Restore/Enable for handlers

// Orchestrator runs the 4-tier degradation chain.
type Orchestrator struct { /* primary, fallback, breakers, eval, cache, tel, log */ }
func (o *Orchestrator) Degrade(ctx, prompt, *Trace) (Response, error)

// BreakerSet bundles transport+semantic CB per model.
type BreakerSet struct { PrimaryTransport, FallbackTransport *circuitbreaker.Breaker
                        PrimarySemantic, FallbackSemantic *quality.SemanticBreaker }

// Chaos owns kill/restore/enable atomic flags.
type Chaos struct { primaryKilled, fallbackKilled atomic.Bool
                    chaosMu atomic.Bool
                    degradedPrimary *provider.DegradedWrapper // EnableDegrade toggles this }
// Bootstrap order in agent.New: build OllamaProvider as raw → wrap in DegradedWrapper
// for the PRIMARY model only → pass wrapped primary + raw fallback to Pipeline →
// give Chaos a reference to the wrapper so EnableDegrade()/DisableDegrade() work.
```

**Migration steps for F3b** (each as a single commit so `git bisect` works):
1. Create `orchestrator/` package with empty `Orchestrator` and `Pipeline` types.
2. Move `degrade`, `tryPrimary`, `tryFallback` from `agent/agent.go` to `orchestrator/orchestrator.go`. `Agent` calls `o.Degrade(...)`.
3. Move `Ask`/`AskBatch` shedder+bulkhead wrapping into `Pipeline.Do`. `Agent.Ask` becomes a one-liner delegate.
4. Move `StreamWithQualityGate` to `orchestrator/stream.go`.
5. Move `KillPrimary`/`RestorePrimary`/`EnableDegradeMode` etc. to `orchestrator/chaos.go`. `Agent` exposes `Chaos()` accessor for handlers.
6. Delete dead code from `agent/agent.go`. Verify field count = 6.

After each step `go test -race ./...` must pass.

### F4 — dashboard embed.FS (detail)

```
api/web/
├── templates/dashboard.html.tmpl     # rendered with html/template
├── static/dashboard.css
├── static/dashboard.js
└── embed.go                          # //go:embed templates static
```

Handler:
```go
//go:embed templates static
var webFS embed.FS

func (h *Handler) dashboard(w http.ResponseWriter, r *http.Request) {
    if devModeEnabled() { /* read from disk */ } else { /* read from embed.FS */ }
    tmpl.Execute(w, viewData{ BuildSHA: build.SHA, Version: build.Version })
}
```

Dev mode (`AGENTSHIELD_DEV=true`) reads from disk so HTML edits are visible without rebuild. Production embed.FS guarantees the binary ships with assets — no missing-file errors at runtime.

### F5 — slog (detail)

- `Config.Logger` includes `Level` (slog.Level) and `Format` (`"text"` / `"json"`). Production default JSON.
- `agent.New(cfg, logger)` requires logger explicitly. No package-level logger.
- Component constructors take `*slog.Logger`, store as `log *slog.Logger`, use `log.With("component", "<name>")`.
- Per-request logger created in `Pipeline.Do` with `trace_id` field, propagated via `context.WithValue` (`internal/logctx` package: `LoggerFromContext`, `ContextWithLogger`).
- Standard log keys defined as constants in `internal/logkeys/keys.go`: `tier`, `model`, `outcome`, `latency_ms`, `quality_score`, `cb_state`, `trace_id`, `component`. Dashboards/alerts in HyperDX or any future log backend will key off stable names.
- **PII guard**: lint rule `forbidigo` denies these substrings in `log.*` argument lists: `prompt`, `response`, `token` (hint: pass them as fields with explicit redaction, not concatenation).

---

## Test strategy

- **Mock LLMProvider** generated via `mockgen` so every component has a uniform mock factory.
- **Property-based test** for `quality.SemanticBreaker.Calibrate`: for any sample of size ≥ MinSamples, computed thresholds must lie within `[mean - 3σ, mean]`. Catches edge cases the current single-input unit test misses.
- **Re-engineer `TestStream_QualityGateSwitchesToFallback`** in F3b migration: replace tokenization-coupled assertions with mock-LLMProvider feeding controlled token sequences.
- `go test -race ./...` mandatory in CI (already present, keep).
- New `forbidigo` rules enforce: no `os.Getenv` outside `config/`, no PII substrings in log calls.

## Risks and mitigations

| Risk | Mitigation |
|------|-----------|
| F3b breaks something subtle in concurrency (context cancellation, atomic ordering) | Six small commits inside F3b; `go test -race` after each; rollback granularity = one commit |
| `provider.LLMProvider.Stream` ownership ambiguity around `out` channel | Documented in interface comment: provider closes `out`. Mock implementations follow same rule. Race detector + drain-test in CI |
| Tools losing capability after `Asker` narrowing | Audit current tool implementations — none call privileged Agent methods. If a tool needs more, expose a wider interface explicitly (named `PrivilegedTool`) rather than pass `*Agent` |
| `agent.New()` no-arg breaking existing callers | Keep convenience: `agent.New()` calls `LoadFromEnv` + `slog.Default()`. New `agent.NewWithConfig(cfg, logger)` is the explicit form |
| Dashboard embed.FS dev/prod divergence (works locally, broken in container) | CI integration test that GETs `/` from compiled binary and asserts response contains `<title>AgentShield`. Fails fast if embed.FS is misconfigured |
| Token estimate of 8 days is optimistic for one-developer-with-LLM speed | 8 days of buffer remain before the deadline. Foundation can slip 50% without endangering the hackathon submission |

## Timeline

| Days | Work |
|------|------|
| 1 | F1 Config |
| 1 | F2 Provider |
| 2 | F3a memory + telemetry + quality + cache extract |
| 2-3 | F3b orchestrator + pipeline + chaos extract |
| 1 | F4 dashboard embed.FS |
| 1 | F5 slog |
| **8** | **Foundation total** |
| 8 | Buffer / spillover |
| 16 | Hackathon deadline |

If Foundation finishes early, the remaining time goes to: multi-provider implementation (Phase D-1), OTEL exporter (Phase D-3), summarization (Phase D-2), load test, demo video, deployment. Each is its own brainstorm → spec → plan cycle, not part of this design.
