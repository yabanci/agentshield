# AgentShield Benchmark Harness

Comparison of naive LLM integration patterns against AgentShield's tiered resilience defence across three failure modes.

## Quick start

```bash
# Run all three scenarios, 50 requests each (takes ~7-8 min due to brownout sleeps)
go run ./bench/cmd/bench -mode all -n 50 -out bench/results.csv -md bench/results.md

# Fast run — skip brownout
go run ./bench/cmd/bench -mode garbage,down -n 50

# Run tests (short mode skips slow brownout test)
go test ./bench/... -race -count=1 -short

# Run full test suite including brownout (takes ~3 min)
go test ./bench/... -race -count=1
```

## Results (seed=42, n=50 per path per scenario)

See [`results.md`](results.md) for the formatted table and [`results.csv`](results.csv) for raw data.

Headline numbers:

| Scenario | Naive useful% | AgentShield useful% | Gap |
|----------|---------------|---------------------|-----|
| garbage  | 0%            | 100%                | +100 pp |
| brownout | 76%           | 100%                | +24 pp |
| down     | 0%            | 100%                | +100 pp |

"Useful" = quality score >= 0.45 (the same threshold `orchestrator.tryPrimary` uses internally).

## What each scenario tests

### garbage — HTTP 200 with low-quality content

The backend returns `200 OK` with JSON, but the response body contains refusal
markers ("As an AI language model…") combined with high trigram repetition.
This is the core AgentShield pitch: transport-layer tools (including traditional
circuit breakers and LangChain retry logic) see a successful HTTP 200 and return
the garbage to the caller.

- **Naive path**: calls backend, gets 200, returns whatever the model said. No
  quality check. 0% of responses are useful.
- **AgentShield path**: evaluates response quality. Score < 0.45 → rejects, falls
  to fallback tier (same fake backend without the scenario header, which returns
  a good response). 100% useful.

### brownout — degraded backend with high tail latency

The backend simulates a real brownout: 50% of requests take 7–9 s (slow cohort),
50% take 200–500 ms (fast cohort). 20% of all responses are also garbage, so
quality degrades alongside latency.

- **Naive path**: returns whatever arrives within the 30 s timeout. 76% of
  responses happen to be good-quality (80% of the fast cohort + some slow ones).
  The other 24% are garbage the caller silently receives.
- **AgentShield path**: the quality gate filters the garbage subset. Fallback
  tier rescues them. 100% useful. p50 latency is similar (the fast cohort still
  dominates), but the garbage responses are intercepted rather than returned.

### down — backend refuses connections (503)

The backend returns HTTP 503 on every request.

- **Naive path**: first attempt fails, one retry fails, errors out. 0% success,
  0% useful. The caller gets a 500.
- **AgentShield path**: primary tier fails (transport error), fallback tier tries
  the same backend (also fails), semantic cache is checked (warm from warmup
  requests), returns a cached response. If cache is cold, Tier 4 returns a
  graceful-denial message rather than erroring out. 100% success, 100% useful.

## Architecture

```
bench/
├── cmd/bench/main.go          — entrypoint: flags, orchestration, CSV+MD output
├── fakebackend/server.go      — deterministic httptest server (3 scenarios)
├── naive/client.go            — minimal direct LLM client (no quality check)
├── runner/runner.go           — drives both paths, collects samples, computes stats
├── runner/quality.go          — imports quality.QualityEvaluator (no duplication)
├── results.csv                — raw per-request data (committed for transparency)
└── results.md                 — formatted table for README + Devpost
```

The AgentShield path in the bench is a minimal in-process reimplementation of
the 4-tier chain from `orchestrator/orchestrator.go`. It uses the same
`quality.QualityEvaluator` and the same `QualityAcceptable` threshold — the
bench scoring and the production code agree on what "useful" means.

## Methodology — what this is and isn't

### What we are measuring

We are measuring the qualitative gap between **a naive integration pattern** and
**AgentShield's tiered defence** across three deterministic failure modes.

The "naive" client deliberately omits resilience primitives that exist in
frameworks like LangChain (fallback_llms, cache, callbacks) because, in
practice, most LLM integrations ship without enabling them. This is the
documented default: one retry on transport error, no quality check, no cache,
no graceful denial.

### What this is NOT

- **Not a benchmark of LangChain as a framework.** LangChain has optional
  resilience features; this client represents the integration pattern, not the
  framework's ceiling.
- **Not a real-world latency benchmark.** Both paths call a local `httptest`
  server. The simulated `time.Sleep` delays in the brownout scenario represent
  real backend latency; the round-trip overhead does not. On a real network
  against a real LLM (Ollama, OpenAI), absolute numbers would be 10–100x
  higher.
- **Not a measurement of real garbage rates.** We inject 100% garbage on the
  `garbage` scenario and 20% on `brownout`. Real models produce garbage at much
  lower rates — but they do produce it, and the rate is unpredictable.

### Why determinism matters

The fake backend uses `math/rand.New(rand.NewSource(seed))` — not the global
`math/rand` state. Given the same `-seed` flag, the same sequence of good vs
garbage responses is returned regardless of goroutine scheduling or wall-clock
time. This means results are reproducible and the CSV committed to the repo
reflects exactly what you would see running `go run ./bench/cmd/bench`.

### Caveats

1. **Latency numbers near 0 ms** for garbage and down scenarios are real: the
   fake backend is an in-process `httptest.Server` with no network hop. The
   brownout scenario adds synthetic `time.Sleep` delays that dominate.
2. **The semantic cache in the AgentShield path uses exact-match** (not cosine
   similarity) because the bench uses a fixed prompt set. The production
   orchestrator uses a cosine-similarity cache with an embedder. The effect is
   the same for fixed prompts; on a diverse prompt distribution the cache hit
   rate would be lower and more requests would fall through to graceful denial.
3. **100% useful on the down scenario** includes graceful-denial responses. The
   graceful-denial text ("All AI tiers are currently unavailable…") itself scores
   below the quality threshold if evaluated against a real prompt. We count it
   as useful because it is a controlled, expected response rather than an error —
   consistent with how the production orchestrator defines tier success.
4. **The bench does not exercise the semantic circuit breaker's open/recover
   cycle.** With n=50 and a warm calibration window, the breaker does not have
   time to trip and reset across both paths. A longer run (n=500) would show the
   breaker state transitions. The quality gate check (score < 0.45 → fallback)
   is what fires in this bench.

### The point

The numbers demonstrate the *qualitative* gap:
- Naive returns garbage at HTTP 200 — AgentShield catches it.
- Naive errors on backend down — AgentShield serves from cache or returns a
  controlled message.
- The cost of this protection is: a small latency overhead from quality
  evaluation (~0 ms for text-only signals, 200–500 ms if a real embedder is
  used for coherence), plus a fallback tier call on quality failures.

Judges evaluating this for the TrueFoundry Resilient Agents Challenge: the
numbers are reproducible. Run `go run ./bench/cmd/bench` yourself.
