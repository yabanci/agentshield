# AgentShield

[![CI](https://github.com/yabanci/agentshield/actions/workflows/ci.yml/badge.svg)](https://github.com/yabanci/agentshield/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/yabanci/agentshield)](https://goreportcard.com/report/github.com/yabanci/agentshield)
[![Go Reference](https://pkg.go.dev/badge/github.com/yabanci/agentshield.svg)](https://pkg.go.dev/github.com/yabanci/agentshield)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](./LICENSE)
[![Security: 14 audit rounds](https://img.shields.io/badge/security%20audits-14%20rounds-brightgreen)](./SECURITY.md)

> 🏆 **Submission for the [TrueFoundry Resilient Agents Challenge](https://devnetwork-ai-ml-hack-2026.devpost.com/) — DevNetwork AI+ML Hackathon 2026**

**An LLM returns HTTP 200. The content is garbage. No existing circuit breaker catches it. AgentShield does.**

Powered by [flowguard](https://github.com/yabanci/flowguard).

> 🎥 **Demo video**: _link goes here once recorded_
> 🌐 **Live dashboard**: _link goes here once deployed_
> 📸 _Dashboard screenshot / chaos-demo GIF goes here_

---

## TL;DR for judges in 30 seconds

- **One unique idea**: a circuit breaker that opens on **LLM quality degradation** (looping, refusal-leak, off-topic), not on transport errors. No other resilience library catches an HTTP 200 with garbage. See [Semantic Circuit Breaker](#semantic-circuit-breaker).
- **One demo button to click**: 🧪 **Enable Degrade** on the dashboard, then 🧪 **Run Compare** to see shielded vs raw side by side. The contrast is the value-prop.
- **One env var for hosted backends**: `LLM_PROVIDER=openai` + `OPENAI_API_KEY` and the same resilience stack runs against OpenAI / Groq / OpenRouter / vLLM. See [Quick Start](#quick-start).
- **All three TrueFoundry failure modes covered**: LLM down → transport CB + fallback tier; LLM brownout → semantic CB (the unique angle); MCP server erroring → per-tool CB on `mcp_lookup` with bundled `cmd/mcp-mock/` to demo it.
- **Engineering depth**: 12 cohesive Go packages, race-clean under `-race -count=10`, fourteen rounds of multi-agent audit closed ~125 findings (per-round summaries in `git log` as `fix: round-N — …` commits; design specs in `docs/superpowers/`).

---

## The problem nobody else solves

Traditional circuit breakers open when a service **goes down** (HTTP 5xx, timeout).  
LLMs fail differently — they stay **up** while silently degrading:

- Model starts hallucinating ("As an AI language model, I cannot…")
- Responses become repetitive loops
- Output becomes semantically unrelated to the prompt
- Response length collapses to a few words

All of this returns **HTTP 200**. No existing circuit breaker catches it.

**AgentShield introduces the Semantic Circuit Breaker**: a circuit breaker that opens on *quality degradation*, not transport errors.

```
Primary model HTTP 200 ✓  BUT  quality score: 18%
                                 ↓
                         Semantic CB: OPEN
                         Transport CB: closed  ←  the key difference
                                 ↓
                         Request routed to fallback
```

---

## The four-tier degradation chain

Every request flows through two independent protection stacks — one for transport, one for quality.

### Degradation chain

```
POST /chat
    │
    ▼ Loadshed (AIMD algorithm — same as TCP congestion control)
    │
    ▼ Bulkhead (interactive: 20 slots │ batch: 5 slots)
    │
    ▼ ┌─ Transport CB (adaptive, trips at >50% error rate over 20-call window) ─┐
      │  Hedge (1.5s delay — fires duplicate if primary is slow)                │
      │  Retry (2× exponential backoff, 300ms base)                             │
      └─ Primary model: llama3.2 ─────────────────────────────────────────────┘
                │
                │ Quality evaluation (outside transport CB — fully independent)
                │   • Repetition score (trigram deduplication)
                │   • Length anomaly (vs rolling baseline)
                │   • Refusal markers (pattern matching)
                │   • Coherence score (cosine similarity via embeddings)
                ▼
         Semantic CB records score → may open independently of transport CB
                │
    ▼ Transport CB (classic, trips after 3 consecutive failures)
      Fallback model: llama3.2:1b
                │
                │ Quality evaluation → Semantic CB for fallback
                ▼
    ▼ Semantic Cache
      nomic-embed-text embeddings + cosine similarity (threshold 0.92)
      "What is Go?" and "Explain Golang" → same cache hit
      10-minute TTL, auto-pruned
                │
    ▼ Graceful Denial — always returns a message, never panics
```

---

## Semantic Circuit Breaker

The core innovation. Two states per model, tracked independently:

| State | Transport CB | Semantic CB |
|---|---|---|
| Normal | closed | healthy |
| Model down | **open** | healthy |
| Model degrading silently | closed | **failing** |
| Both | open | failing |

**Quality signals** (no external APIs, all local):

| Signal | Method | Max penalty |
|---|---|---|
| Repetition | Trigram deduplication — detects looping responses | 0.45 |
| Length anomaly | Deviation from rolling baseline (absolute min: 10 chars) | 0.25 |
| Refusal markers | 9 known refusal/persona-leak phrases ("as an AI...", "I cannot...") | 0.40 |
| Coherence | Cosine similarity to prompt via embeddings | 0.20 |
| Language mismatch | Latin vs CJK script disagreement with the prompt | 0.30 |

(Penalties stack additively, then the final score is clipped to `[0,1]`.)

**What "quality degradation" actually means here.** The semantic CB catches
*structural* degradation — loops, refusals to answer, persona breaks, gibberish,
off-topic responses, wrong-language output. It does NOT detect factual
hallucination (a fluent, well-formed, wrong answer). Defending against factual
accuracy requires ground-truth retrieval or entailment checks outside the
scope of a local resilience layer. The pitch is honest about what the breaker
catches; production users layering AgentShield should add factual checks
separately if their use case demands it.

**Adaptive calibration**: the semantic CB observes the first 20 healthy
responses and sets thresholds at `mean ± 1σ` (degraded) and `mean ± 2σ`
(failing). A 0.05 floor on σ keeps perfectly consistent models from
self-calibrating to an over-tight breaker. Bad samples (below 0.45) are
excluded from the calibration window so enabling degrade mode early in the
session can't poison the baseline. A model scoring 0.95 ± 0.03 ends up at
roughly degraded < 0.90, failing < 0.85; a noisier model gets a wider band.
No manual tuning.

---

## ReAct Agent with Tool Use

`POST /react` runs a full Reason + Act loop. The LLM can invoke tools; each tool has its own circuit breaker. Two cooperative features keep long sessions from degrading:

- **Per-session tool result cache** — identical (case- and whitespace-normalized) tool calls within one chat turn are answered from an in-memory LRU cache instead of making another round-trip. Eliminates the "same `mcp_lookup` three times in a row" thrash pattern. Configurable via `AGENTSHIELD_TOOL_CACHE_ENABLED` / `AGENTSHIELD_TOOL_CACHE_MAX_ENTRIES` (default 64 entries).
- **Transcript summarization** — when the running Thought/Action/Observation history grows past `AGENTSHIELD_REACT_MAX_TRANSCRIPT_TOKENS` (default 6000), the oldest 50% is replaced by a single-paragraph LLM summary before the next reasoning call, keeping prompt size bounded and latency stable across deep reasoning chains.

```
prompt → LLM reasons → decides to call tool → tool executes → LLM processes result → ...
              │                │ transcript check           │ cache check (Part A)
         degradation chain     └─ maybe summarize (Part B)  └─ skip round-trip on hit
         (4 tiers above)
```

Built-in tools (no external APIs):

| Tool | What it does |
|---|---|
| `calculate` | Math expression evaluator (`2^10 + sqrt(144)`) |
| `get_time` | Current time in any timezone |
| `search_docs` | Searches embedded resilience knowledge base |
| `check_system` | Returns live AgentShield metrics |
| `mcp_lookup` ★ | Calls an external [MCP server](https://modelcontextprotocol.io/) (`weather`, `currency`, etc.). Registered when `MCP_URL` is set. |

★ = closes the TrueFoundry brief's third failure mode (MCP server erroring out).

### MCP integration

`mcp_lookup` is wired through the same per-tool circuit breaker as the
built-in tools, so an erroring MCP server degrades gracefully instead of
hanging ReAct loops. Bundled with a tiny mock server at
`cmd/mcp-mock/` for demos:

```bash
# Terminal 1 — fake MCP server with /mcp/kill + /mcp/restore toggles
go run ./cmd/mcp-mock                         # listens on :8081

# Terminal 2 — AgentShield wired against it
MCP_URL=http://localhost:8081 go run .

# Ask the agent to use it
curl -X POST localhost:8080/react -d '{"prompt": "Use mcp_lookup to get the weather in Berlin"}'

# Now simulate MCP failure — the tool's CB opens after 3 errors
curl -X POST localhost:8081/mcp/kill
curl -X POST localhost:8080/react -d '{"prompt": "Use mcp_lookup for Berlin"}'    # CB closed: tries, fails
curl -X POST localhost:8080/react -d '{"prompt": "Use mcp_lookup for Paris"}'      # 2nd failure
curl -X POST localhost:8080/react -d '{"prompt": "Use mcp_lookup for Tokyo"}'      # 3rd: CB opens
curl -X POST localhost:8080/react -d '{"prompt": "Use mcp_lookup for Madrid"}'     # CB rejects without calling MCP
```

Point `MCP_URL` at any real MCP server (Anthropic's reference impl, fastmcp,
or your own) and the same resilience guarantees apply — the CB wraps the
HTTP boundary, not anything spec-specific.

---

## Benchmark: AgentShield vs Naive Integration

Reproducible numbers from the deterministic bench harness in `bench/`. Each scenario ran 50 requests per path. Full methodology in [`bench/README.md`](bench/README.md).

| Scenario | Naive useful% | AgentShield useful% | What happened |
|----------|---------------|---------------------|---------------|
| **garbage** (HTTP 200, bad content) | 0% | **100%** | Naive returns whatever the model said; AgentShield's quality gate intercepts score < 0.45 and routes to fallback |
| **brownout** (p95 = 8.8s, 20% garbage) | 76% | **100%** | Naive silently delivers the 24% garbage; AgentShield filters them to fallback |
| **down** (503 / connection refused) | 0% | **100%** | Naive errors out; AgentShield serves from cache then falls back to graceful denial |

Full table with latency percentiles: [`bench/results.md`](bench/results.md) | Raw CSV: [`bench/results.csv`](bench/results.csv)

To reproduce:

```bash
go run ./bench/cmd/bench -mode all -n 50 -seed 42
```

---

## Resilience Score

A single 0–100 metric that aggregates all resilience dimensions. Five components, 20 points each. Updates live in the dashboard.

```
Score: 95 / 100   Grade: A

  Transport Health   20 / 20   (both CBs closed)
  Semantic Quality   18 / 20   (primary avg quality 91%)
  Cache Efficiency   17 / 20   (38% hit rate + cost savings)
  Availability       20 / 20   (0% graceful denials)
  Latency            20 / 20   (primary p95 < 1s)
```

| Component | Source | Max |
|---|---|---|
| Transport Health | transport CB state (primary + fallback) | 20 |
| Semantic Quality | semantic CB state (primary 12pts, fallback 8pts) | 20 |
| Cache Efficiency | cache fill ratio + cost-savings bonus | 20 |
| Availability | served-vs-denied ratio | 20 |
| Latency | primary p95 latency bands (< 1s → 20, < 3s → 16, …) | 20 |

During a chaos scenario: **100 → 41 → 78 → 95**. Judges can watch the number recover in real-time as each component heals.

---

## Cost Savings

Tracks estimated token costs per tier and computes savings.

```
Primary (llama3.2):   $0.0018  spent
Fallback (llama3.2:1b): $0.0004  spent (67% cheaper per token)
Cache:                $0.0000  spent

Saved by cache:     $0.0022  (would have cost primary rate)
Saved by fallback:  $0.0003  (delta vs primary rate)
Total saved:        $0.0025  (52% savings rate)
```

---

## Per-Request Resilience Trace

Every `Ask()` and `React()` generates a trace showing exactly what happened:

```json
GET /trace/tr_a1b2c3d4

{
  "id": "tr_a1b2c3d4",
  "prompt": "explain circuit breakers",
  "total_ms": 1847,
  "final_tier": "fallback",
  "steps": [
    {
      "tier": "primary",
      "latency_ms": 1230,
      "transport_cb": "closed",
      "semantic_cb": "failing",
      "quality_score": 0.18,
      "quality_signals": ["repetition", "refusal_marker"],
      "outcome": "semantic_failure"
    },
    {
      "tier": "fallback",
      "latency_ms": 617,
      "transport_cb": "closed",
      "semantic_cb": "healthy",
      "quality_score": 0.91,
      "outcome": "success"
    }
  ]
}
```

Trace ID is included in every response. Clickable `📋 trace` link in the dashboard.

---

## Webhook Notifications

Configure a webhook to receive alerts when circuit breaker states change:

```bash
POST /config/webhook
{"url": "https://your-ops-system/alert"}
```

Payload on state change:
```json
{
  "event": "semantic_cb_failing",
  "model": "primary",
  "prev_state": "degraded",
  "new_state": "failing",
  "reason": "rolling avg quality 38% < failing threshold 45%",
  "avg_quality": 0.38,
  "timestamp": "2026-04-30T14:32:01Z"
}
```

---

## Streaming Quality Gate

`GET /chat/stream` streams tokens via SSE with an inline quality gate.  
If refusal/persona-leak markers are detected in the first 120 tokens, the stream aborts and continues from fallback — automatically.

```
[token1][token2]...[token47]  ← from primary
⚡ quality gate triggered at token 47 — switching to fallback
[token1][token2]...           ← from fallback
```

---

## Quick Start

### Local Ollama (free, default)

```bash
# 1. Install Ollama
brew install ollama

# 2. Pull models (~4GB total)
ollama pull llama3.2
ollama pull llama3.2:1b
ollama pull nomic-embed-text   # for semantic cache + quality coherence

# 3. Start Ollama
ollama serve

# 4. Run AgentShield
OLLAMA_URL=http://localhost:11434 go run .

# 5. Open dashboard
open http://localhost:8080
```

### Hosted OpenAI-compatible backend

Any provider speaking the `/v1/chat/completions` contract works — OpenAI,
Groq, Together, OpenRouter, vLLM, Mistral, llama.cpp's server.

```bash
export LLM_PROVIDER=openai
export OPENAI_API_KEY=sk-...
export OPENAI_BASE_URL=https://api.openai.com/v1      # or api.groq.com/openai/v1
export OPENAI_PRIMARY_MODEL=gpt-4o-mini
export OPENAI_FALLBACK_MODEL=gpt-4o-mini              # cheaper model is the safety net
# Optional: keep embeddings on local Ollama (free) or run them through OpenAI:
# export OPENAI_EMBED_MODEL=text-embedding-3-small

go run .
```

## Configuration

| Env var | Default | Purpose |
|---|---|---|
| `PORT` | `8080` | HTTP listen port |
| `OLLAMA_URL` | `http://localhost:11434` | Ollama API root |
| `LLM_PROVIDER` | `ollama` | `ollama` or `openai` (any OpenAI-compatible endpoint) |
| `OPENAI_API_KEY` | — | Bearer token when `LLM_PROVIDER=openai` |
| `OPENAI_BASE_URL` | `https://api.openai.com/v1` | Override for Groq, OpenRouter, vLLM, etc. |
| `OPENAI_PRIMARY_MODEL` | `gpt-4o-mini` | Chat model when in openai mode |
| `OPENAI_FALLBACK_MODEL` | `gpt-4o-mini` | Fallback chat model |
| `OPENAI_EMBED_MODEL` | (empty) | If set, embeddings go through OpenAI; else local Ollama |
| `MCP_URL` | (empty) | When set, registers the `mcp_lookup` ReAct tool against this MCP server. Use `http://localhost:8081` with the bundled `cmd/mcp-mock/` |
| `AGENTSHIELD_AUTH_TOKEN` | (empty) | Bearer token. Gates `/demo/*`, `/sessions/*`, `/trace/{id}`, `/config/webhook` when set |
| `AGENTSHIELD_ALLOW_HTTP_WEBHOOK` | `false` | Allow `http://` webhook destinations |
| `AGENTSHIELD_ALLOW_PRIVATE_WEBHOOK` | `false` | Allow webhook URLs resolving to private/loopback IPs |
| `AGENTSHIELD_TRUSTED_PROXIES` | (empty) | Comma-separated CIDRs whose `X-Forwarded-For` is honored |
| `LOG_LEVEL` | `info` | `debug` / `info` / `warn` / `error` |
| `LOG_FORMAT` | `text` | `text` or `json` |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | (empty) | OTLP/gRPC collector endpoint e.g. `localhost:4317`. Empty = no-op tracer (tracing disabled) |
| `OTEL_EXPORTER_OTLP_INSECURE` | `true` | Skip TLS on the OTLP exporter. Set to `false` in production |
| `OTEL_EXPORTER_OTLP_TIMEOUT` | `10s` | Export batch timeout (Go duration string) |

---

## Demo Scenarios

### Scenario 1: Transport failure
```
1. Send prompts → Resilience Score: 94, primary responds
2. POST /demo/kill → primary killed
3. Next prompt → fallback takes over (transport CB)
4. POST /demo/restore → auto-recovery
```

### Scenario 2: Semantic degradation (the unique demo)
```
1. Send prompts → Score 94, primary quality ~91%
2. POST /demo/degrade → primary returns garbage (HTTP 200 ✓)
3. Quality score drops → semantic CB opens
4. Transport CB stays CLOSED — HTTP was never the problem
5. POST /demo/restore-quality → semantic CB recovers
```

### Scenario 3: Automated chaos
```
GET  /demo/chaos/stream  →  SSE stream; Score drops 94 → 41 → 94 automatically
                            4 scripted phases, all tiers exercised
```

### Scenario 4: Shielded vs raw, side-by-side
```
POST /demo/compare  →  fires the same prompt through the full degradation
                       chain AND directly to the LLM in parallel; dashboard
                       renders both responses with latency and quality.
                       Pairs perfectly with /demo/degrade — raw returns
                       garbage, shielded routes to fallback.
```

---

## API Reference

```
POST /chat                         {"prompt": "..."}
POST /chat           (batch)       X-Priority: batch
GET  /chat/stream?prompt=...       SSE streaming with quality gate
POST /react                        {"prompt": "...", "session_id": "..."}

GET  /status                       full live snapshot
GET  /score/history                resilience-score sparkline points
GET  /trace/{id}              ★    per-request resilience trace
GET  /metrics                      Prometheus metrics
GET  /health                       primary provider reachability (alias)
GET  /health/live                  process liveness probe
GET  /health/ready                 readiness probe (checks LLM provider)
GET  /auth/required                tells the dashboard whether auth is enabled

GET  /sessions                ★    list active sessions
GET  /sessions/{id}           ★    session message history
DELETE /sessions/{id}         ★    clear session

POST /demo/kill               ★    simulate primary transport failure
POST /demo/restore            ★
POST /demo/kill-fallback      ★    simulate fallback transport failure
POST /demo/restore-fallback   ★
POST /demo/degrade            ★    simulate primary quality degradation
POST /demo/restore-quality    ★
POST /demo/compare                 side-by-side shielded vs raw (rate-limited)
GET  /demo/chaos/stream       ★    SSE stream of automated chaos scenario

POST /config/webhook          ★    {"url": "..."}
GET  /config/webhook          ★
DELETE /config/webhook        ★
```

★ = gated by `AGENTSHIELD_AUTH_TOKEN` when configured; open otherwise.

---

## Prometheus Metrics

```
agentshield_requests_total{tier}               counter
agentshield_request_duration_seconds{tier}     histogram (p50/p95/p99)
agentshield_cb_state{model}                    gauge  0=closed 1=half-open 2=open
agentshield_semantic_cb_state{model}           gauge  0=healthy 1=degraded 2=failing
agentshield_quality_score{model}               gauge  0.0–1.0
agentshield_cache_size                         gauge
agentshield_cache_hits_total                   counter
agentshield_loadshed_total                     counter
agentshield_bulkhead_full_total{type}          counter
agentshield_hedge_fires_total                  counter
agentshield_webhook_dropped_total             counter
```

> **Observability:**
> - A production-grade Grafana 10 dashboard (14 panels across 4 rows: request flow, quality, latency, defenses) and 7 Prometheus alert rules with runbook URLs live in [`deploy/grafana/`](./deploy/grafana/) — import the JSON in one drag-and-drop, apply the PrometheusRule CRD or `rule_files:` entry, and every signal above has a panel and at least one alert threshold.
> - **Distributed traces via OpenTelemetry** — set `OTEL_EXPORTER_OTLP_ENDPOINT` to any OTLP/gRPC collector (Jaeger, Tempo, HyperDX, Grafana Agent). Each request shows a parent server span plus tier-breakdown child spans (`agentshield.tier.primary`, `.fallback`, `.cache`, `.degrade`), quality score, CB state, and ReAct iteration + tool call spans — flame-graph visibility at every hop.

---

## Project Structure

```
main.go                          HTTP server, graceful shutdown, auth-warn

agent/                           Public API surface
  agent.go                       Agent struct, Ask/AskBatch/Compare/React/Stream
  react.go                       ReAct loop (reason → act → observe)
  tool.go                        Built-in tools + expression evaluator

orchestrator/                    Degradation chain (extracted from agent)
  orchestrator.go                tryPrimary → tryFallback → cache → graceful denial
  stream.go                      Streaming with inline quality gate
  breakers.go                    BreakerSet bundle for primary/fallback × transport/semantic
  chaos.go                       Kill / Restore / Degrade toggles

provider/                        Pluggable LLM backends behind LLMProvider
  provider.go                    LLMProvider + Embedder interfaces
  ollama.go                      Ollama HTTP client (generate, stream, embed)
  openai.go                      OpenAI-compatible adapter (Groq, OpenRouter, vLLM, ...)
  degraded.go                    DegradedWrapper — chaos decorator

quality/                         Semantic quality evaluation
  evaluator.go                   5-signal scoring (no external APIs)
  breaker.go                     SemanticBreaker + adaptive calibration

cache/
  semantic.go                    Embedding-based cache (cosine + exact fallback)
  metrics.go                     Prometheus gauges/counters for the cache

memory/                          Per-process state (sessions, traces, tier counts)
  store.go                       Top-level Store bundling sub-stores
  sessions.go                    Session-history store with TTL eviction
  traces.go                      Per-request trace store
  score_history.go               Sparkline data for the resilience score
  tier.go                        Tier enum (primary/fallback/cache/degraded)

telemetry/                       Prometheus metrics + cost + latency + score
  metrics.go                     Counter / Histogram / Gauge definitions
  cost.go                        Token estimation + cost-savings tracking
  latency.go                     Rolling-window P50/P95/P99 per tier
  score.go                       Resilience Score (5 components × 20 points)
  webhook.go                     Webhook dispatcher for CB state changes
  webhook_events.go              Event payload shape

config/
  config.go                      Typed runtime configuration
  env.go                         os.Getenv → Config (the only os import in the project)

api/
  handler.go                     HTTP routes (chat, react, stream, demo, sessions, ...)
  auth.go                        Bearer-token middleware
  ratelimit.go                   Per-IP sliding window with X-Forwarded-For guard
  webhook_validate.go            SSRF defense (DNS resolve, scheme allowlist)
  web/templates/dashboard.html.tmpl
  web/static/dashboard.{css,js}  Single-page dashboard, no build step

internal/
  logkeys/                       Stable log field names
  logctx/                        Trace-ID context helpers

truefoundry/
  deploy.py                      TrueFoundry Python SDK deployment
  service.yaml                   TrueFoundry YAML manifest
```

---

## Tests

```bash
make test            # go test ./... -race -count=1   (all packages, race detector on)
make coverage        # cross-package coverage (the honest number — see note below)
make vuln            # govulncheck ./...              (zero known CVEs as of v0.2.0)
make lint            # golangci-lint run ./...
make smoke           # live HTTP smoke against a running agentshield on :8080
```

All tests use `httptest.Server` — no running Ollama required.

**Coverage note.** Default `go test -cover ./...` only counts coverage inside
each test's own package, so the orchestrator package (covered by `agent/`
integration tests, not by its own unit tests) reports a misleading ~10%.
`make coverage` uses `-coverpkg=./...` to attribute coverage to where the
code actually lives, which yields the honest **71.2% project-wide** total.

---

## Docker

```bash
docker compose up          # builds and starts AgentShield
                           # Ollama must run natively (Metal GPU)
```

`OLLAMA_URL` defaults to `http://host.docker.internal:11434` in the Docker image.

---

## Deploy to TrueFoundry

```bash
pip install truefoundry
tfy login
tfy secret create --name OLLAMA_URL --value "http://<ollama-host>:11434"
python truefoundry/deploy.py --workspace <WORKSPACE_FQN>
```

AgentShield exposes `/health` for liveness/readiness probes and `/metrics` for Prometheus — both wired into the service manifest.

---

## Submission

Built for the [TrueFoundry Resilient Agents Challenge](https://devnetwork-ai-ml-hack-2026.devpost.com/) — DevNetwork AI+ML Hackathon 2026.

Resilience library: **[github.com/yabanci/flowguard](https://github.com/yabanci/flowguard)**
