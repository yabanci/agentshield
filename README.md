# AgentShield

**Resilient LLM Agent — four-tier graceful degradation powered by [flowguard](https://github.com/yabanci/flowguard)**

AgentShield wraps Ollama LLM calls with production-grade resilience primitives. When your primary model fails, requests automatically cascade through a degradation chain — no errors exposed to users.

```
POST /chat  →  Primary model (llama3.2)       ← circuit breaker + retry
                     ↓ on failure
             Fallback model (llama3.2:1b)      ← circuit breaker
                     ↓ on failure
             In-memory response cache          ← 10-min TTL
                     ↓ on cache miss
             Graceful denial message           ← always available
```

## Why this matters

LLM calls fail constantly in production — GPU OOM, model loading, timeouts, rate limits. Standard retry loops aren't enough: a circuit breaker stops hammering a broken endpoint, a fallback model keeps users unblocked, and a cache absorbs repeated queries during incidents.

AgentShield composes all four patterns into a single `agent.Ask()` call backed by the open-source `flowguard` library.

## Quick start

**Prerequisites:** [Ollama](https://ollama.ai) installed and running.

```bash
# Pull the models (one-time)
ollama pull llama3.2
ollama pull llama3.2:1b

# Run AgentShield
go run . 

# Open the dashboard
open http://localhost:8080
```

## Demo: live failure injection

The dashboard has one-click controls to simulate failures:

1. Open `http://localhost:8080`
2. Send a prompt → response comes from **primary** tier
3. Click **Kill Primary Model** → next prompt routes to **fallback** tier
4. Ask the same prompt again → returns from **cache** tier (instant, no model needed)
5. Click **Restore Primary** → circuit closes, primary resumes

## API

```
POST /chat          {"prompt": "your question"}
GET  /status        circuit breaker states, cache size, error rate
POST /demo/kill     simulate primary model failure
POST /demo/restore  restore primary model
GET  /health        Ollama reachability check
```

## Architecture

```
main.go              HTTP server, graceful shutdown
agent/
  agent.go           Degradation chain orchestration
  ollama.go          Ollama HTTP client
  cache.go           In-memory response cache (SHA-256 keyed)
api/
  handler.go         HTTP route handlers
  dashboard.go       Single-page HTML dashboard
```

### Resilience primitives used

| Primitive | Source | Applied to |
|---|---|---|
| Adaptive circuit breaker | `flowguard/circuitbreaker` | Primary model (trips at >50% error rate over 20-call window) |
| Classic circuit breaker | `flowguard/circuitbreaker` | Fallback model (trips after 3 consecutive failures) |
| Exponential retry | `flowguard/retry` | Primary model (3 attempts, 200ms base) |

## Running tests

```bash
go test ./...
```

Tests use an `httptest.Server` to mock Ollama — no running Ollama needed.

## Submission

Built for the [TrueFoundry Resilient Agents Challenge](https://devnetwork-ai-ml-hack-2026.devpost.com/) at DevNetwork AI+ML Hackathon 2026.
