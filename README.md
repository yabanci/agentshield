# AgentShield

> 🏆 **Submission for the [TrueFoundry Resilient Agents Challenge](https://devnetwork-ai-ml-hack-2026.devpost.com/) — DevNetwork AI+ML Hackathon 2026**

**Production-grade resilience middleware for LLM agents — powered by [flowguard](https://github.com/yabanci/flowguard)**

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

## How it works

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
                │   • Hallucination markers (pattern matching)
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

| Signal | Method | Weight |
|---|---|---|
| Repetition | Trigram deduplication — detects looping responses | 0.45 |
| Length anomaly | Deviation from rolling baseline (absolute min: 10 chars) | 0.25 |
| Hallucination markers | 9 known refusal/hallucination phrases | 0.40 |
| Coherence | Cosine similarity to prompt via embeddings | 0.20 |

**Adaptive calibration**: the semantic CB observes the first 20 responses and automatically sets thresholds to `mean ± 1σ` and `mean ± 2σ`. A model consistently scoring 0.95 ± 0.03 gets tight thresholds (degraded < 0.92, failing < 0.89). A model scoring 0.70 ± 0.15 gets looser ones. No manual tuning.

---

## ReAct Agent with Tool Use

`POST /react` runs a full Reason + Act loop. The LLM can invoke tools; each tool has its own circuit breaker.

```
prompt → LLM reasons → decides to call tool → tool executes → LLM processes result → ...
              │                                        │
         degradation chain                     tool's own CB
         (4 tiers above)
```

Built-in tools (no external APIs):

| Tool | What it does |
|---|---|
| `calculate` | Math expression evaluator (`2^10 + sqrt(144)`) |
| `get_time` | Current time in any timezone |
| `search_docs` | Searches embedded resilience knowledge base |
| `check_system` | Returns live AgentShield metrics |

---

## Resilience Score

A single 0–100 metric that aggregates all resilience dimensions. Updates live in the dashboard.

```
Score: 94 / 100   Grade: A

  Transport Health   25 / 25   (both CBs closed)
  Semantic Quality   23 / 25   (primary avg quality 91%)
  Cache Efficiency   21 / 25   (38% hit rate)
  Availability       25 / 25   (0% graceful denials)
```

During a chaos scenario: **94 → 41 → 78 → 94**. Judges can watch the number recover in real-time.

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
      "quality_signals": ["repetition", "hallucination_marker"],
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
If hallucination markers are detected in the first 120 tokens, the stream aborts and continues from fallback — automatically.

```
[token1][token2]...[token47]  ← from primary
⚡ quality gate triggered at token 47 — switching to fallback
[token1][token2]...           ← from fallback
```

---

## Quick Start

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
POST /demo/chaos  →  Watch Score drop from 94 → 41 → 94 automatically
                     4 scripted phases, all tiers exercised
```

---

## API Reference

```
POST /chat                         {"prompt": "..."}
POST /chat           (batch)       X-Priority: batch
GET  /chat/stream?prompt=...       SSE streaming with quality gate
POST /react                        {"prompt": "...", "session_id": "..."}

GET  /status                       full live snapshot
GET  /trace/{id}                   per-request resilience trace
GET  /metrics                      Prometheus metrics
GET  /health                       Ollama reachability

GET  /sessions                     list active sessions
GET  /sessions/{id}                session message history
DELETE /sessions/{id}              clear session

POST /demo/kill                    simulate primary transport failure
POST /demo/restore
POST /demo/kill-fallback           simulate fallback transport failure
POST /demo/restore-fallback
POST /demo/degrade                 simulate primary quality degradation
POST /demo/restore-quality
GET  /demo/chaos/stream            SSE stream of automated chaos scenario

POST /config/webhook               {"url": "..."}
GET  /config/webhook
DELETE /config/webhook
```

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
```

---

## Project Structure

```
main.go                   HTTP server, graceful shutdown
agent/
  agent.go                Degradation chain + all public API
  ollama.go               Ollama HTTP client (generate, stream, embed)
  cache.go                Semantic cache (cosine sim + exact fallback)
  quality.go              Quality evaluator (4 signals, no external APIs)
  semantic_breaker.go     Semantic circuit breaker (adaptive calibration)
  cost.go                 Token estimation + cost savings tracking
  score.go                Resilience Score (0–100 composite)
  react.go                ReAct agent loop
  tool.go                 Built-in tools + expression evaluator
  session.go              Conversation session store (TTL eviction)
  trace.go                Per-request resilience trace store
  webhook.go              Webhook dispatcher for CB state changes
  chaos.go                Automated chaos scenario runner
  metrics.go              Prometheus metric definitions
  *_test.go               Unit tests (mock Ollama server, no real LLM needed)
api/
  handler.go              All HTTP route handlers + SSE
  dashboard.go            Single-page dashboard (Chart.js, no build step)
truefoundry/
  deploy.py               TrueFoundry Python SDK deployment
  service.yaml            TrueFoundry YAML manifest
```

---

## Tests

```bash
go test ./...                   # all tests
go test -race ./...             # with race detector
go test -race -count=5 ./...    # stress test
```

All tests use `httptest.Server` — no running Ollama required.

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
