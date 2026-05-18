# Devpost submission — AgentShield

> Draft for the TrueFoundry Resilient Agents Challenge.
> Copy each section into the matching Devpost field at submission time.
> Length tuned for the Devpost form (Inspiration ~75 words, What-it-does
> ~200, How-we-built-it ~200, Challenges ~150, etc.). Tweak before paste.

---

## Tagline (one line, shows in the gallery)

**Your LLM returned HTTP 200. The output is garbage. Now what?**

(Interrogative — interrupts the gallery scroll; implies a problem the
reader has personally hit. Alternates:
- _"The first circuit breaker that opens on LLM quality degradation, not transport errors."_
- _"Catches the HTTP 200 with garbage — so you don't ship it."_)

---

## Inspiration

Every resilience library — Resilience4j, tenacity, polly, every retry decorator on PyPI — opens its circuit breaker on what it knows: timeouts, 5xx, connection errors. None of them catch the LLM failure mode that actually happens in production: the model returns HTTP 200, the wire health is perfect, and the content is garbage. "As an AI language model, I cannot…" repeated five times. A trailing trigram loop. An off-topic ramble. Existing CBs sit closed and the user gets the garbage. AgentShield exists to close that gap.

---

## What it does

AgentShield is a Go HTTP service that wraps any LLM (Ollama, OpenAI, Groq, OpenRouter, vLLM — anything speaking the OpenAI chat-completions contract) in a four-tier degradation chain plus a **semantic circuit breaker** that opens on quality, not transport.

Two breakers per model run independently:
- **Transport CB** — classic, trips on 5xx / timeouts.
- **Semantic CB** — five-signal quality evaluator (repetition trigrams, length anomaly, refusal markers, prompt-coherence via embeddings, language mismatch) feeds a rolling window with adaptive `mean ± σ` calibration. When the rolling avg drops below the failing threshold, the breaker opens — even though every response was HTTP 200.

Above that: bulkhead + AIMD load shedder + hedged requests (P95-style) + retry with backoff + 4-tier fallback (primary → fallback model → semantic embedding cache → graceful denial) + per-tool circuit breaker on a ReAct agent.

The TrueFoundry brief names three failure modes — **LLM down**, **LLM brownout**, **MCP server erroring**. AgentShield handles all three: transport CB + fallback tier for the first, semantic CB for the second (the unique one), per-tool CB on the bundled `mcp_lookup` ReAct tool for the third.

A single dashboard shows the **Resilience Score** (5 × 20 = 100, components: Transport / Quality / Cache / Availability / Latency), a side-by-side **shielded vs raw compare**, a live **latency histogram** (p50 / p95 / p99 per tier), a **flame-trace timeline** for every request, and an automated **chaos demo** that drops the score 100 → 41 → back to 95 in under a minute.

---

## How we built it

Go 1.26.3, no production dependencies outside `flowguard` (our own resilience-primitives library, also open-sourced at github.com/yabanci/flowguard, v0.3.0) and Prometheus client. 12 cohesive packages, acyclic dep graph, race-clean under `go test -race -count=10`.

The semantic CB and the quality evaluator are pure-Go and pure-local — no external API calls, no third-party model. The five quality signals run in 1-2ms each. Adaptive calibration captures the first 20 healthy responses and learns `mean ± σ` thresholds with a `std` floor (so a perfectly consistent model doesn't self-calibrate to a too-tight breaker) and a ceiling clamp (so a calibration-poisoning attacker can't pre-seed an over-strict band). The streaming variant aborts the primary mid-response and continues from fallback when refusal markers appear in the first 120 tokens.

Multi-provider abstraction is one Go interface (`LLMProvider`) with two implementations and a `DegradedWrapper` decorator for chaos injection. Swapping backends is one env var: `LLM_PROVIDER=openai`.

The MCP integration is a 5th ReAct tool wired through the existing per-tool CB; we ship `cmd/mcp-mock/` (a 120-line standalone server) so the demo shows a real MCP outage tripping the tool's CB in three calls.

---

## Challenges we ran into

Three rounds (1, 2, 3) of multi-agent code audits — backend, security, QA, code review — surfaced ~50 findings in the first week alone. The hardest one wasn't a bug, it was a calibration math error: a model scoring 0.92 ± 0.01 was calibrating its failing threshold to 0.90, so the *more reliable* the model, the *more aggressive* the breaker. Fixed by clamping σ at a 0.05 floor.

The second-hardest: getting the demo right. The semantic CB is the unique pitch, but pre-fix the dashboard buried it as the second section under generic Kill Primary. Reordered to lead with it, marked "★ killer demo," and added a side-by-side Compare button so the value-prop is visible in one click.

Threats from an adversarial round 8: a malicious visitor on a public live URL could `curl /demo/kill` in a loop and wedge the demo for everyone. Fixed by a non-resetting 5-minute auto-restore timer (with a generation-counter handshake to close a subtle Restore→Kill race that round 9 caught).

---

## Accomplishments we're proud of

- **The semantic CB is genuinely novel** for this hackathon track — every other resilience library opens on transport.
- **All three TrueFoundry failure modes covered explicitly** — LLM down (transport CB + fallback), LLM brownout (semantic CB), MCP erroring (per-tool CB with bundled mock for the demo).
- **Multi-provider abstraction** — the same resilience stack runs against Ollama, OpenAI, Groq, OpenRouter, vLLM. One env var.
- **10 audit rounds, ~100 findings closed**, including 4 stdlib CVEs (XSS escaper bypass in `html/template` reachable via the dashboard template path, closed by toolchain bump to go1.26.3). The audit logs and reports are in the repo at `docs/superpowers/`.
- **Race-clean test suite** at `go test -race -count=20`. Sustained-load stress + fuzz tests.

---

## What we learned

LLMs don't fail like services. The retry/CB intuitions every backend engineer carries — "open on errors, close on success" — map badly to a system that confidently returns HTTP 200 with the wrong content. The honest scope of a semantic CB is *structural* degradation: refusal-leak, loops, off-topic, wrong-language. Factual hallucination needs ground-truth retrieval or entailment checks; we documented that boundary clearly so production users don't mistake the scope.

A surprising amount of resilience design is **demo design**. The chaos scenario, the side-by-side compare, the flame trace timeline — none of these affect the running system but all of them affect whether a judge or operator can *see* what the resilience stack is doing.

---

## What's next

- **OpenTelemetry trace propagation** to outbound LLM/MCP calls so cross-service spans connect.
- **Grafana dashboard JSON + Prometheus alert rules** shipped alongside the Helm chart.
- **Tool-cache and conversation summarization** for ReAct sessions over 20 messages.
- **Real factual-accuracy detector** (entailment-based) as an optional sixth quality signal for use cases where structural-degradation detection isn't enough.
- **Bench against LangChain + langchain-resilience** to publish a head-to-head.

---

## Built with

`go` · `flowguard` (own library) · `Ollama` · `OpenAI-compatible API` · `Prometheus` · `Chart.js` · `MCP` · `Server-Sent Events` · `Go 1.26.3` · `Docker` · `TrueFoundry`

---

## Try it yourself

```bash
git clone https://github.com/yabanci/agentshield && cd agentshield
ollama pull llama3.2 && ollama pull llama3.2:1b && ollama pull nomic-embed-text
ollama serve &
go run .
# open http://localhost:8080, click 🧪 Enable Degrade, then ⚖️ Run Compare
```

Or against any OpenAI-compatible backend:

```bash
LLM_PROVIDER=openai OPENAI_API_KEY=sk-... go run .
```

Live demo: _<deployed TrueFoundry URL goes here once deployed>_
Demo video: _<unlisted YouTube link goes here once recorded>_
