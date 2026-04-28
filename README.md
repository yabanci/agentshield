# AgentShield

> 🏆 **Submission for [TrueFoundry Resilient Agents Challenge](https://devnetwork-ai-ml-hack-2026.devpost.com/) — DevNetwork AI+ML Hackathon 2026**

**Production-grade resilience middleware for LLM agents — powered by [flowguard](https://github.com/yabanci/flowguard)**

AgentShield demonstrates every major reliability pattern from the "Resilience Engineering" playbook, applied to LLM inference. When models fail, timeout, or overload — users never see an error.

---

## Degradation chain

```
POST /chat
    │
    ▼
[Loadshed]   adaptive AIMD limit (TCP congestion control algorithm)
    │
    ▼
[Bulkhead]   interactive (20 slots) | batch (5 slots) — isolated concurrency
    │
    ▼
[CircuitBreaker → Hedge → Retry]   primary model: llama3.2
    │  Adaptive CB trips at >50% error rate over 20-call window
    │  Hedge: if no response in 1.5s, fires duplicate request in parallel
    │  Retry: 2x exponential backoff (300ms base)
    │
    │  ← circuit opens or all retries fail
    ▼
[CircuitBreaker]   fallback model: llama3.2:1b
    │  Classic CB: trips after 3 consecutive failures
    │
    │  ← circuit opens
    ▼
[Semantic Cache]   nomic-embed-text embeddings + cosine similarity (threshold 0.92)
    │  "What is Go?" and "Explain the Go language" → same cache hit
    │  10-minute TTL, auto-pruned
    │
    │  ← cache miss
    ▼
[Graceful Denial]  always available, never panics
```

---

## Resilience primitives

All resilience primitives come from **[flowguard](https://github.com/yabanci/flowguard)** — an open-source Go resilience library.

| Pattern | Implementation | Applied to |
|---|---|---|
| Adaptive circuit breaker | [`flowguard/circuitbreaker`](https://github.com/yabanci/flowguard/tree/main/circuitbreaker) | Primary model |
| Classic circuit breaker | [`flowguard/circuitbreaker`](https://github.com/yabanci/flowguard/tree/main/circuitbreaker) | Fallback model |
| Exponential retry | [`flowguard/retry`](https://github.com/yabanci/flowguard/tree/main/retry) | Primary model (2 retries) |
| Hedged requests | [`flowguard/hedge`](https://github.com/yabanci/flowguard/tree/main/hedge) | Primary model (1.5s delay) |
| Bulkhead | [`flowguard/bulkhead`](https://github.com/yabanci/flowguard/tree/main/bulkhead) | Interactive vs batch isolation |
| Adaptive load shedding | [`flowguard/loadshed`](https://github.com/yabanci/flowguard/tree/main/loadshed) | All requests (AIMD algorithm) |
| Semantic cache | cosine similarity on Ollama embeddings | Response reuse |

---

## Quick start

```bash
# 1. Install Ollama
brew install ollama

# 2. Pull models (one-time, ~4GB total)
ollama pull llama3.2
ollama pull llama3.2:1b
ollama pull nomic-embed-text

# 3. Start Ollama
ollama serve

# 4. Run AgentShield
go run .

# 5. Open dashboard
open http://localhost:8080
```

---

## Live demo

The dashboard has one-click failure injection:

1. Send a prompt → **primary** tier responds
2. **Kill Primary** → next prompt routes to **fallback**
3. Ask the same prompt → returns from **semantic cache** (instant)
4. **Kill Fallback** too → graceful denial message
5. **Restore Primary** → circuit recovers automatically after 15s probe

Toggle **Streaming mode** to see tokens appear in real-time via SSE.

---

## API

```
POST /chat                     {"prompt": "..."}
GET  /chat/stream?prompt=...   SSE token stream
GET  /status                   live snapshot of all resilience layers
POST /demo/kill                simulate primary failure
POST /demo/restore             restore primary
POST /demo/kill-fallback       simulate fallback failure
POST /demo/restore-fallback    restore fallback
GET  /metrics                  Prometheus metrics
GET  /health                   Ollama reachability
```

**Batch priority** (lower concurrency quota):
```
POST /chat
X-Priority: batch
```

---

## Prometheus metrics

```
agentshield_requests_total{tier}             counter
agentshield_request_duration_seconds{tier}   histogram
agentshield_cb_state{model}                  gauge (0=closed 1=half-open 2=open)
agentshield_cache_size                       gauge
agentshield_cache_hits_total                 counter
agentshield_loadshed_total                   counter
agentshield_bulkhead_full_total{type}        counter
agentshield_hedge_fires_total                counter
```

---

## Project structure

```
main.go              HTTP server, graceful shutdown
agent/
  agent.go           Degradation chain orchestration
  ollama.go          Ollama HTTP client (generate, stream, embed)
  cache.go           Semantic cache (cosine similarity + exact fallback)
  metrics.go         Prometheus metric definitions
  agent_test.go      Unit tests with mock Ollama server
api/
  handler.go         HTTP route handlers + SSE streaming
  dashboard.go       Single-page dashboard (Chart.js)
```

---

## Tests

```bash
go test ./...
```

All tests use `httptest.Server` — no running Ollama required.

---

## Submission

Built for the [TrueFoundry Resilient Agents Challenge](https://devnetwork-ai-ml-hack-2026.devpost.com/) — DevNetwork AI+ML Hackathon 2026.

Resilience library: [github.com/yabanci/flowguard](https://github.com/yabanci/flowguard)
