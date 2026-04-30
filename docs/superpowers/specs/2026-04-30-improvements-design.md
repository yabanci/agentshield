# AgentShield Improvements — Design

**Date**: 2026-04-30
**Goal**: Win the TrueFoundry Resilient Agents Challenge (deadline 2026-05-28)
**Scope**: Phases A + B + C (Phase D deferred to post-submission backlog)

---

## Non-goals

These items are deferred to a post-hackathon backlog:

- Conversation summarization (D-1)
- Tool result caching (D-2)
- W3C trace context propagation / OpenTelemetry (D-3)
- OpenAPI spec generation (D-4)

Reason: ~6-8h of work, low marginal hackathon score impact, no demo visibility.

---

## Phase A — Quick fixes (~3-4h)

Bugs found during repeated audit passes. All have known root cause and fix.

### A-1. `main.go` doesn't call `a.Stop()` on graceful shutdown
**Symptom**: TraceStore + SessionStore `time.NewTicker` goroutines outlive `srv.Shutdown()`. Process can't exit cleanly; goroutines hold references to stores → no GC.

**Fix**: After `srv.Shutdown(ctx)`, call `a.Stop()`.

### A-2. `startChaos` uses `context.Background()` instead of server context
**Symptom**: Chaos scenario runs forever even after server starts shutdown. The `chaosMu` semaphore never releases.

**Fix**: Pass a server-lifetime context through to `StartChaos`. On shutdown, this context is cancelled and the chaos sleep goroutines (already context-aware) terminate.

### A-3. `cache.set()` blocks the response while computing embedding
**Symptom**: Every successful request that writes to cache calls `embedder()` synchronously, adding 200-500ms before returning to the user.

**Fix**: Spawn a goroutine for embedding inside `cache.set()`. The cache entry is appended without embedding; a follow-up goroutine fetches the embedding and updates the entry under lock. Until the embedding is set, only exact-match `get()` lookups succeed for that entry.

### A-4. `quality.go` coherence is symmetric (false negatives)
**Symptom**: Cosine similarity between `embed("What is Go?")` and `embed("Python is great")` may be ~0.5 because both are about programming languages. Symmetric similarity isn't direction-aware: it doesn't measure "does the response answer the prompt?"

**Fix**: Add an asymmetric coherence check. Compare `embed(prompt)` to `embed(response[:N])` (first N tokens of response — early divergence is a stronger signal than overall topic similarity). Keep the symmetric check as a secondary signal with lower weight.

**Trade-off**: Direction-aware similarity is harder to compute correctly. We'll keep the existing cosine sim and add a "topic-relevance" penalty: if prompt mentions a domain term and response doesn't, penalize.

**Decision**: Defer the deep fix (retrieval-trained embedding model) to post-submission backlog. Quick fix here: penalize responses whose first-sentence cosine sim is much lower than overall cosine sim (sign of off-topic preamble). Threshold: if `sim(prompt, first_sentence) < 0.5 * sim(prompt, full_response)`, apply 0.15 penalty.

### A-5. `tool.go` no per-tool timeout
**Symptom**: A misbehaving tool (e.g., infinite loop in calculate, slow search) hangs the entire ReAct loop until parent context expires.

**Fix**: Each `ToolRegistry.Execute` wraps the call in `context.WithTimeout(ctx, toolTimeout)` (default 10s, configurable per-tool).

### A-6. No max prompt length validation
**Symptom**: A 100MB prompt body is accepted, allocated, and processed. Memory blow-up risk.

**Fix**: In `chat` handler, reject prompts > `maxPromptBytes` (default 32KB) with HTTP 413.

### A-7. `score.go` integer division truncation
**Symptom**: `size * 25 / 40` truncates. For `size=39`, gives `24` not `24.375`. Visually fine but technically wrong.

**Fix**: Use `int(float64(size)*25/40 + 0.5)` for proper rounding.

### A-8. Quality evaluator doesn't detect language switches
**Symptom**: English prompt + Chinese response = 100% quality (no signal triggers).

**Fix**: Detect non-ASCII character ratio. If response has >50% non-ASCII chars and prompt has <10% non-ASCII chars, flag as `language_mismatch` signal (weight 0.30).

**Trade-off**: This is heuristic. False positives possible for prompts requesting non-English content. Acceptable for English-primary deployment.

### A-9. `tools` map field not protected (theoretical race)
**Symptom**: `ToolRegistry.tools` is written once during `newToolRegistry` and never modified. No actual race, but the type doesn't enforce immutability.

**Fix**: Document immutability in the package comment. No code change needed (already safe).

---

## Phase B — Hackathon-grade features (~5-6h)

Visible improvements judges will see.

### B-1. Input token tracking
**Why**: Current cost tracker counts only output tokens. Real production cost is dominated by input (60-80% in conversation use). Numbers are misleading.

**Design**:
- `CostTracker.Record` takes both `inputText` and `outputText` (was just `responseText`).
- For React mode, input is the full conversation context (prompt + history); for Ask, it's just the prompt.
- Pricing constants split into `inputPerMToken` and `outputPerMToken`. OpenAI/Anthropic typically charge ~3-5x more for output, but for our Llama-class estimates we'll use 50% input / 100% output rates (Groq pricing model).
- Dashboard cost panel shows two columns: "Input tokens" and "Output tokens".

### B-2. Latency in Resilience Score
**Why**: A model that's "up but 30s slow" isn't healthy. Current score considers a slow primary as fully healthy.

**Design**:
- Track p95 latency per tier in a rolling window (last 100 requests, in-memory).
- New score component (or absorbed into Transport Health):
  - p95 < 1s: full points
  - p95 1-3s: -2 points
  - p95 3-10s: -5 points
  - p95 > 10s: -10 points
- Add `latency_p95_ms` per tier to Status.
- Display in dashboard: "Primary p95: 1.2s ✓" badge.

**Trade-off**: 25-point Transport Health budget gets crowded. Either rebalance to Transport Health: 20pts, Latency: 10pts (over 4 components), or absorb into Transport with cap.

**Decision**: Rebalance components to 5×20pts: Transport, Quality, Cache, Availability, **Latency** — total still 100.

### B-3. Drift detection + recalibration
**Why**: Calibration runs once at startup. If the model is updated/swapped, the baseline is stale and thresholds are wrong.

**Design**:
- After calibration, continue tracking a long-term rolling mean (window of 100 scores).
- If `|long_term_mean - baseline_mean| > 0.20`, mark `Calibration.Drift = true` and clear thresholds back to defaults until enough new healthy samples accumulate.
- Webhook event: `calibration_drift_detected`.
- Dashboard badge: orange "drift detected" state on the calibration line.

### B-4. SSE heartbeat
**Why**: Streaming connections through proxies/load balancers can be killed after 30-60s of silence between tokens. No keepalive.

**Design**:
- After each token sent, schedule a heartbeat timer (15s).
- If no real token sent for 15s, send `:heartbeat\n\n` (SSE comment, ignored by clients).
- Reset timer on every token write.

### B-5. Resilience Score history sparkline
**Why**: Single number is good; a sparkline showing the last 60 score values during chaos is the demo highlight.

**Design**:
- Backend: keep a ring buffer of last 60 score values + timestamps.
- Endpoint: `GET /score/history` → JSON array of `{score, ts}`.
- Dashboard: ECharts/Chart.js sparkline above the score number, updating every 3s.

### B-6. Trace viewer modal in dashboard
**Why**: Currently `📋 trace` link opens raw JSON in a new tab. A modal with the step list rendered as cards is dramatically better demo UX.

**Design**:
- Click `📋 trace` → fetch `/trace/{id}` → render as a vertical stepper with tier icons, latency bars, quality scores, and signals.
- Dropdown of recent traces (last 10) at top of dashboard.

### B-7. Streaming switch event visualization
**Why**: When the streaming quality gate fires, the dashboard log says "stream switched" but the UI doesn't show *where* in the stream it happened.

**Design**:
- When `Switched: true` event arrives via SSE, insert a visual divider in the response box: `⚡ switched to fallback (quality gate at token 47)`.
- Continue rendering tokens below the divider with a different background color (yellow tint).

---

## Phase C — Production hardening (~3-4h)

Less flashy but adds credibility.

### C-1. Auth on demo + config endpoints
**Why**: `/demo/kill`, `/demo/degrade`, `/config/webhook` are unauthenticated. Anyone with the dashboard URL can DOS or set up SSRF.

**Design**:
- Optional bearer token via `AGENTSHIELD_AUTH_TOKEN` env var.
- If set, all `/demo/*` and `/config/*` endpoints require `Authorization: Bearer <token>`.
- If unset, endpoints are open (current behavior — preserves dev experience).
- Dashboard auto-detects via `/auth/required` endpoint and prompts for token if needed.

### C-2. Per-IP rate limiting
**Why**: One client can saturate all bulkhead slots, denying others.

**Design**:
- Add a `flowguard/ratelimit.NewSlidingWindow(60, time.Minute)` per source IP via in-memory `map[string]*Limiter`.
- Response: HTTP 429 if exceeded.
- LRU eviction (max 10k IPs tracked).

### C-3. Webhook URL allowlist (SSRF prevention)
**Why**: `/config/webhook` accepts any URL. Internal network probing risk.

**Design**:
- Validate URL: must be `https://` (or `http://` with `AGENTSHIELD_ALLOW_HTTP_WEBHOOK=true`).
- Reject IPs in private ranges (RFC 1918) and localhost unless dev mode.
- Reject `file://`, `gopher://`, etc. (not URL.Scheme `http`/`https`).

### C-4. Liveness vs readiness separation
**Why**: `/health` checks Ollama. K8s would restart the pod if Ollama is briefly unreachable, which is wrong — the pod is fine, the dependency isn't.

**Design**:
- `/health/live` — returns 200 if process is alive (no checks). Liveness probe.
- `/health/ready` — returns 200 only if Ollama is reachable. Readiness probe.
- Existing `/health` aliases to `/health/ready` for backwards compat.

### C-5. Dashboard auth
**Why**: Dashboard exposes degrade/kill controls. If auth is enabled (C-1), dashboard should require it.

**Design**:
- Token stored in `localStorage` after login prompt.
- Sent as `Authorization: Bearer <token>` on every `/status`, `/chat`, etc.
- 401 → re-prompt for token.

---

## Implementation order within phases

Within each phase, items are independent and can ship in any order. Recommended sequence:

**A**: A-1, A-2 (smallest, foundational) → A-6, A-5 (small) → A-7, A-9 (trivial) → A-3 (async embedding) → A-4, A-8 (quality evaluator extensions)

**B**: B-1 (input tokens, isolated) → B-2 (latency in score, isolated) → B-3 (drift detection, isolated) → B-4 (SSE heartbeat, isolated) → B-5 (score history endpoint) → B-6 (trace modal UI) → B-7 (streaming switch UI)

**C**: C-1 (auth foundation) → C-5 (dashboard auth, depends on C-1) → C-3 (webhook validation) → C-2 (rate limiting) → C-4 (liveness/readiness)

---

## Testing strategy

- Each item ships with unit tests where applicable.
- Race detector + 5x count for every test run.
- Lint must pass.
- After each phase: full integration test against mock Ollama, deploy to TrueFoundry sandbox, verify dashboard.

---

## Risk + trade-offs

- **A-3 (async embedding)** introduces a brief window where a cache entry exists without an embedding. Exact match still works; semantic match misses for that entry until the goroutine completes. Acceptable.
- **A-4 (asymmetric coherence)** is heuristic; full direction-aware solution needs a different embedding model trained for retrieval. The simple penalty rule we use here may have false positives on legitimate "the answer is X" preamble. Tunable via threshold.
- **B-2 (latency in score)** changes the score range/distribution. Existing tests that assert "score = X" need adjustment.
- **C-1 (auth)** must default to OFF or every existing user breaks. We'll feature-flag via env var presence.

---

## Success criteria

- Phase A: race-detector clean, 0 lint issues, all known bugs closed.
- Phase B: chaos demo shows score history sparkline, cost numbers include input tokens, drift detection visible in dashboard during a contrived "model swap" scenario.
- Phase C: hackathon submission deployable to TrueFoundry with auth enabled; security review clean for SSRF and DoS vectors.
