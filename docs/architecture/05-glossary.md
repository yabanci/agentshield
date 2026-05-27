# 5. Glossary

[← Previous: Request Lifecycle](04-request-lifecycle.md) · [Back to index](README.md)

---

Every term used in this folder (and in the main README), defined in plain
English, alphabetical. The goal is that you never have to already know a word to
understand the docs.

---

### AIMD

**Additive Increase, Multiplicative Decrease.** A simple, famous rule for
adjusting a limit under changing conditions: when things go well, raise the
limit a *little* (add a small amount); when things go badly, cut it *a lot*
(multiply by something less than 1, e.g. halve it). It's the same maths TCP uses
to control internet congestion. AgentShield's [load shedder](#load-shedding)
uses AIMD to decide how many requests it's willing to accept right now — it
creeps the allowance up while healthy and slashes it fast when errors spike.

### Brownout

When a service is **up but degraded** rather than fully down. The word comes
from electrical grids: a *blackout* is total loss of power; a *brownout* is a
voltage sag where your lights dim but don't go out. For an [LLM](#llm-large-language-model),
a brownout means it still answers (HTTP 200), but the answers have quietly gone
bad — loops, refusals, off-topic text. This is the failure mode AgentShield's
[semantic circuit breaker](#semantic-circuit-breaker) exists to catch.

### Bulkhead

A limit on **how many requests can be running at the same time.** Named after
the watertight compartments in a ship's hull: if one floods, the bulkheads stop
the water from spreading and sinking the whole ship. In software, a bulkhead
caps concurrency so one traffic burst can't consume all your memory/connections
and take everything down. AgentShield keeps separate bulkheads for *interactive*
traffic (20 slots) and *batch* traffic (5 slots), so a flood of batch jobs can't
starve real users.

### Calibration poisoning

An attack on a system that *learns* its normal behavior from early data. If you
can control the first samples the system sees, you can teach it a wrong "normal."
For AgentShield: the [semantic breaker](#semantic-circuit-breaker) learns each
model's typical quality from its first 20 good responses. An attacker who fed it
20 hand-crafted *perfect* answers could trick it into thinking "normal" is near
perfection — making it trip on ordinary, fine 80%-quality answers later.
AgentShield defends against this by *clamping* how strict the learned thresholds
are allowed to get.

### Circuit breaker

A safety wrapper around a call to another service. It counts failures, and if
too many happen, it **"opens"** — stops attempting the call for a while and fails
fast (or returns a backup) instead. After a cooldown it lets one test call
through; if that works it **"closes"** and resumes normal traffic. Named after
the electrical breaker in your home that trips to prevent a fire. See
[The Problem](01-the-problem.md) for the full walk-through. AgentShield runs two
kinds: a [transport](#semantic-circuit-breaker) one (network health) and a
[semantic](#semantic-circuit-breaker) one (content quality).

### Cosine similarity

A number from −1 to 1 that says **how similar two pieces of meaning are**, by
comparing their [embeddings](#embedding). Geometrically it's the cosine of the
angle between two vectors: 1 means "pointing the same way" (very similar), 0
means "unrelated," −1 means "opposite." AgentShield uses it two ways: to check
whether an answer is *on-topic* relative to the question (the coherence quality
signal), and to find *similar past questions* in the [semantic cache](#semantic-cache).

### Embedding

A way to turn text into a **list of numbers** (a "vector") that captures its
*meaning*, produced by a small AI model. The key property: texts with similar
meaning get similar number-lists, even if they use different words. "What is
Go?" and "Explain Golang" end up close together. Comparing embeddings with
[cosine similarity](#cosine-similarity) is how a computer measures "do these two
texts mean roughly the same thing?" without understanding language the way a
human does.

### Graceful denial

AgentShield's last-resort tier (Tier 4). When every model is down and the
[cache](#semantic-cache) has nothing useful, instead of crashing or hanging, it
returns a fixed, polite message: *"All AI tiers are currently unavailable.
Please try again shortly."* From the user's perspective the request *succeeded*
— it got a clear, instant, human-readable response — even though there's no real
answer behind it. The opposite of a stack trace or a 30-second timeout.

### Hedged request

A trick to fight slow "tail" requests. If a call is taking unusually long (in
AgentShield, past 1.5 seconds), fire a **second, identical** call and use
whichever one comes back first. Like asking two people the same question when
the first is taking forever — you take the faster answer and ignore the other.
Costs an extra call occasionally, but dramatically smooths out the worst-case
latency.

### HTTP 200

The standard "**OK / success**" response code on the web. The crux of
AgentShield's whole premise: an LLM returning `HTTP 200` only means "a response
was delivered." It says *nothing* about whether the text inside is any good. A
[brownout](#brownout) is exactly a stream of 200-OK responses full of garbage —
which is why watching status codes alone (what traditional
[circuit breakers](#circuit-breaker) do) misses it entirely.

### LLM (Large Language Model)

The kind of AI that powers ChatGPT, Claude, Gemini, Llama, etc. You give it
text (a "prompt") and it generates text back. It's the thing AgentShield wraps
and protects. LLMs are unusually good at *failing while looking fine* — see
[brownout](#brownout) — which is the reason this project exists.

### Load shedding

Deliberately **rejecting some requests** when you're overloaded, so the ones you
*do* accept stay fast. Counterintuitive but vital: a server that tries to accept
everything under overload slows *all* requests until it collapses; one that
cleanly turns away the excess keeps serving the rest. AgentShield's load shedder
uses an [AIMD](#aimd) rule to decide the current limit.

### MCP (Model Context Protocol)

An open standard (from Anthropic) for **how an AI agent talks to external tools
and data sources** — weather APIs, databases, file systems, etc. — in a uniform
way. AgentShield includes an MCP-backed tool (`mcp_lookup`) for its
[ReAct](#react-reason--act) agent, wrapped in its own [circuit breaker](#circuit-breaker)
so a broken MCP server trips that one tool instead of hanging the agent. This is
how AgentShield covers the challenge brief's third failure mode ("MCP server
erroring out").

### Observability

The umbrella term for **being able to see what your system is doing** from the
outside, via three kinds of signal: **metrics** (numbers over time, like
"requests per second"), **traces** (the path of one request through the system),
and **logs** (text records of events). AgentShield emits all three. See
[Prometheus](#prometheus) and [OpenTelemetry](#opentelemetry-otel).

### Ollama / Groq / OpenAI-compatible

The actual LLM backends AgentShield can talk to.
- **Ollama** runs models *locally* on your own machine (free, private, used as
  the default for local development).
- **Groq** is a hosted service that runs open models extremely fast.
- **"OpenAI-compatible"** means any provider that speaks the same API shape as
  OpenAI's `/v1/chat/completions` — which includes Groq, OpenRouter, vLLM,
  Mistral, and OpenAI itself. AgentShield supports all of them through one
  interface; switching is a single environment variable (`LLM_PROVIDER`).

### OpenTelemetry (OTel)

The industry-standard toolkit for **traces and metrics**. It lets you record,
for each request, a tree of "[spans](#span)" (this called that, which called this…) and
ship them to a viewer (Jaeger, Grafana Tempo, HyperDX) where you see a
flame-graph of where time went. AgentShield wraps its outbound calls and adds a
span per [tier](#resilience-score) tried, tagged with quality scores and breaker
states.

### Percentile (p50 / p95 / p99)

A way to describe a *spread* of measurements (usually latencies) without being
fooled by averages. "**p95** = 400 ms" means **95% of requests were faster than
400 ms** (and the slowest 5% were worse). p50 is the median (half faster, half
slower); p99 is the slowest 1% — the "tail." Why not just use the average? One
very slow request can drag the average up and hide that most requests were fine,
or a few fast ones can hide a bad tail. Percentiles tell you "what does a *typical*
user see (p50)" vs "what does an *unlucky* user see (p95/p99)." AgentShield's
[hedging](#hedged-request) exists specifically to improve the p95/p99 tail.

### Prometheus

The de-facto standard **metrics** system. Your service exposes a `/metrics` page
of numbers (counters, gauges, histograms); Prometheus periodically "scrapes" it
and stores the history; you graph and alert on it (often via Grafana).
AgentShield exposes metrics like `agentshield_requests_total{tier="fallback"}`
and `agentshield_quality_score{model="primary"}`.

### Quality signal

One of the **five cheap, local checks** AgentShield runs on every LLM answer to
score its quality (0 to 1). Each looks for a specific kind of brokenness:
**repetition** (looping, via [trigrams](#trigram)), **length anomaly** (too
short / too long), **refusal markers** ("As an AI language model…"),
**coherence** (off-topic, via [embeddings](#embedding)), and **language
mismatch** (wrong script). Penalties stack. A total below 0.45 with at least one
signal firing counts as a quality failure. Crucially, none of these call another
AI — they're fast string/maths checks, a millisecond or two each.

### Quantized

A **compressed** version of an AI model. The model's internal numbers are
stored at lower precision (e.g. 8-bit or 4-bit instead of 16-bit), which makes
it much smaller and cheaper to run — at the cost of some quality. Providers
sometimes quietly serve a quantized model under heavy load to save GPU. From the
outside you can't see the swap; you only notice the answers got a bit worse. It's
one of the real-world causes of a [brownout](#brownout).

### ReAct (Reason + Act)

A pattern for making an LLM **use tools** instead of just chatting. The model
loops: *Reason* about what to do → *Act* by calling a tool → *Observe* the
tool's result → reason again, until it can answer. AgentShield's ReAct agent has
tools like a calculator, a clock, a doc search, and an external
[MCP](#mcp-model-context-protocol) lookup — each with its own
[circuit breaker](#circuit-breaker).

### Resilience Score

AgentShield's single **health number, 0–100**, for an operator's at-a-glance
view. It's the sum of five components worth 20 points each:

| Component | Measures |
|---|---|
| Transport Health | are the network breakers closed? |
| Semantic Quality | are the quality breakers healthy? |
| Cache Efficiency | how well is the cache absorbing load? |
| Availability | what fraction of requests got a real answer vs a denial? |
| Latency | is the primary model's p95 response time fast? |

During the demo's "chaos" scenario it visibly drops (100 → ~41) and recovers, so
you can *watch* resilience happen.

### Retry / Backoff

**Retry**: if a call fails, try it again — failures are often transient.
**Backoff**: wait longer between each retry (e.g. 300 ms, then 600 ms…) so you
don't pile on a struggling service. AgentShield retries primary calls twice with
exponential backoff — but *skips* retrying on "connection refused," because if
the server simply isn't there, retrying can't help and only delays falling
through to the next tier.

### Semantic cache

A cache that matches entries by **meaning**, not exact text. A normal cache
needs the exact same key to hit; a semantic cache turns each question into an
[embedding](#embedding) and finds past questions that are *close in meaning*
(via [cosine similarity](#cosine-similarity), threshold 0.92). So "What is Go?"
can be answered from a cached "Explain Golang." In AgentShield it's Tier 3 —
when both models are unreachable, a semantically-similar past answer can still be
served instantly. Entries expire after 10 minutes.

### Semantic circuit breaker

**The novel core of AgentShield.** A [circuit breaker](#circuit-breaker) that
trips on **content quality** instead of network errors. It scores each answer
with the five [quality signals](#quality-signal), tracks a rolling average, and
"opens" (state: *failing*) when quality consistently drops — *even though every
response was HTTP 200 and the network breaker stays closed*. It has three states
(healthy → degraded → failing), recovers cautiously, and tunes its own
thresholds to each model (see [The Two Circuit Breakers](03-two-circuit-breakers.md)).
"Semantic" just means "relating to meaning." No other resilience library has
this — it's the project's contribution.

### Span

One **timed step inside a [trace](#trace)**. If a trace is the whole story of a
request, a span is one chapter: "called the primary model — took 240 ms." Spans
nest (a parent span contains child spans), which is what produces the
"flame-graph" picture — a stack of nested bars showing where time went.
AgentShield creates one span per [tier](#resilience-score) it tries, tagged with
that tier's quality score and breaker state. The term comes from
[OpenTelemetry](#opentelemetry-otel).

### SSE (Server-Sent Events) / streaming

A way for a server to **push a response piece by piece** over a single HTTP
connection, instead of sending it all at once at the end. It's how chat UIs show
the answer "typing out" [token](#token) by token. AgentShield supports streaming
and adds a twist: a **quality gate** inside the stream — if it spots refusal
markers in the first ~120 tokens, it *aborts the stream mid-flight* and restarts from the
fallback model, so the user doesn't watch garbage scroll by.

### Standard deviation (σ)

A measure of **how spread out a set of numbers is** around their average. Low σ
= the numbers are tightly clustered (consistent); high σ = they're all over the
place (variable). AgentShield's [semantic breaker](#semantic-circuit-breaker)
uses it during self-calibration: it sets its "failing" threshold at `mean − 2σ`,
i.e. "more than two standard deviations below this model's normal." It floors σ
at 0.05 so an extremely consistent model doesn't get a hair-trigger breaker.

### Token

The **unit an LLM reads and writes** — not quite a word, not quite a letter.
Models chop text into "tokens": common words are one token ("cat"), rarer or
longer words split into pieces ("tokenization" → "token" + "ization"). Roughly,
1 token ≈ 0.75 English words. It matters here for two reasons: LLMs generate text
*one token at a time* (which is why [streaming](#sse-server-sent-events--streaming)
can show an answer "typing out"), and providers bill *per token*. AgentShield's
streaming quality gate inspects the first ~120 tokens to catch a bad answer early.

### Trace

The recorded **story of one request** as it moved through the system: which
[tiers](#resilience-score) were tried, each one's latency and quality score, and
which breakers were in which state. Every AgentShield response carries a
`trace_id`; `GET /trace/{id}` returns the full story, which the dashboard draws
as a timeline. Invaluable for answering "*why* did this request get the fallback
answer?" See the [Request Lifecycle](04-request-lifecycle.md) page for examples.

### Trigram

A sequence of **three consecutive words** (or tokens). Counting repeated
trigrams is a cheap, reliable way to detect when an LLM is **looping** — stuck
repeating itself. If "as an ai" and "ai language model" appear over and over,
the repetition [quality signal](#quality-signal) fires. (More generally an
"n-gram" is n consecutive items; "tri" = three.)

---

## Quick map: jargon → "it just means…"

| If a doc says… | It just means… |
|---|---|
| "the breaker opens" | "it stopped trying that thing for a while" |
| "transport layer" | "the network / HTTP plumbing" |
| "semantic" | "about meaning / content" |
| "degradation chain" | "the list of backups to fall through" |
| "fall through to the next tier" | "that backup failed, try the next one" |
| "rolling average" | "average of the last few, updated as new ones arrive" |
| "p95 latency" | "95% of requests were faster than this" |
| "hedge" | "fire a duplicate if the first is slow, take whichever wins" |
| "load shed" | "reject some traffic on purpose so the rest stays fast" |
| "graceful denial" | "a polite 'try again' instead of a crash" |
| "calibration" | "learning what normal looks like, automatically" |

---

[← Previous: Request Lifecycle](04-request-lifecycle.md) · [Back to index](README.md)
