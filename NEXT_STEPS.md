# NEXT_STEPS — AgentShield Hackathon Submission

**Hackathon**: [TrueFoundry Resilient Agents Challenge](https://devnetwork-ai-ml-hack-2026.devpost.com/) (DevNetwork AI+ML Hackathon 2026)
**Deadline**: 2026-05-28
**Last session**: 2026-04-30
**Status**: Engineering complete. 14 bugs fixed across 4 audit passes. 17 features shipped (Phases A+B+C of `docs/superpowers/specs/2026-04-30-improvements-design.md`). All tests green. CI green. Repo: https://github.com/yabanci/agentshield

---

## What's actually left to win

### 🔴 #1 — Documentation drift fix (~30 min)

The README is out of sync with reality. Judges read READMEs.

- [ ] README's Resilience Score section still says "4 components × 25pts" — we changed to **5×20pts** (added Latency component). Update the score table + sparkline image to match.
- [ ] Add new endpoints to the API list:
  - `GET /score/history` — sparkline data
  - `GET /health/live` — process liveness (always 200)
  - `GET /health/ready` — Ollama-aware readiness
  - `GET /auth/required` — dashboard auth probe
  - `POST /demo/degrade`, `POST /demo/restore-quality` — semantic CB demo
- [ ] Add Configuration section documenting env vars:
  - `PORT` (default 8080)
  - `OLLAMA_URL` (default http://localhost:11434)
  - `AGENTSHIELD_AUTH_TOKEN` — bearer token; if set, gates `/demo/*` and `/config/*`
  - `AGENTSHIELD_ALLOW_HTTP_WEBHOOK=true` — let webhook URLs use http://
  - `AGENTSHIELD_ALLOW_PRIVATE_WEBHOOK=true` — let webhook URLs target private/loopback IPs (dev only)
- [ ] Update degradation chain ASCII diagram to mention SSE heartbeat in streaming
- [ ] Mention drift detection + resilience score sparkline as features

### 🔴 #2 — Demo video (1-2h)

This is THE thing that wins or loses the hackathon. Suggested arc:

```
0:00  Dashboard at Score: 100. "AgentShield is a resilience layer for LLM agents."
0:15  Send normal prompt. Show response, click trace icon → modal with full step list.
0:30  THE KILLER MOMENT: click 🧪 Enable Degrade. Send prompt. Response is gibberish.
       Pause on dashboard: transport=closed ✅ but quality=failing ✗.
       "Traditional circuit breakers don't catch this. AgentShield does."
1:00  Click ▶ Run Chaos Demo. Watch sparkline drop 100 → 41 → 78 → 94 live.
       Cost savings number ticks up in real time.
1:30  Switch to Streaming mode, prompt that triggers quality gate.
       Show orange divider mid-response: "⚡ quality gate triggered, switched to fallback".
1:45  Cut to README + flowguard repo: "Powered by my own resilience library."
2:00  End card with GitHub URL + Devpost submission link.
```

### 🟡 #3 — Live deployment to TrueFoundry (1-2h)

Right now `truefoundry/deploy.py` and `service.yaml` exist but no live instance.

```bash
pip install truefoundry
tfy login
tfy secret create --name OLLAMA_URL --value "http://<your-ollama-host>:11434"
python truefoundry/deploy.py --workspace <YOUR_WORKSPACE_FQN>
```

Optional: also configure `AGENTSHIELD_AUTH_TOKEN` so judges can't kill the model.

### 🟡 #4 — Devpost submission text (2-3h)

Template:
- **Tagline**: "The first circuit breaker that understands LLM quality"
- **Inspiration**: Production LLMs fail silently — HTTP 200 with garbage is invisible to traditional tooling
- **What it does**: 5-component resilience stack with semantic CB, adaptive calibration, drift detection, cost tracking, ReAct agent
- **How we built it**: Go + own flowguard library + Ollama; the Resilience Score (5×20pts) gives judges one number to grok everything
- **Challenges**: 4 audit passes, 14 bugs found, 17 improvements shipped, calibrating quality thresholds
- **Accomplishments**: Semantic CB is unique; adaptive thresholds, async embedding, streaming quality gate
- **What we learned**: Production AI needs different observability than traditional services
- **What's next**: Multi-provider LLM support (OpenAI/Groq/Anthropic), conversation summarization, OpenTelemetry, OpenAPI spec (Phase D backlog)

### 🟢 #5 — Polish (30 min)

- [ ] Dashboard screenshot at top of README
- [ ] 30-second GIF of the chaos demo
- [ ] Update GitHub repo description (one-liner under repo name)
- [ ] Pin agentshield + flowguard repos on your GitHub profile

---

## Hard rules to remember

- **Never push directly to main** — always feature branch + PR (per CLAUDE.md)
- **flowguard** is the dependency library; agentshield consumes it. Both maintained separately.
- **Phase D items are intentionally deferred** to post-submission backlog. Do NOT scope-creep them in.

## State of the codebase

- 27 fields in `Agent` struct (God object growing — known debt, post-submission refactor)
- `dashboard.go` is ~900 lines mixed HTML/CSS/JS in a Go string literal (known debt)
- 5×20 Resilience Score: Transport / Quality / Cache / Availability / **Latency**
- Auth defaults to OFF (preserves dev experience). Production deploy should set `AGENTSHIELD_AUTH_TOKEN`.

## Phase D — explicitly deferred

These are NOT for the hackathon. After the deadline, in priority order:

1. Multi-provider LLM support — abstract `ollamaClient` behind an `LLMProvider` interface; support OpenAI/Groq/TrueFoundry inference
2. Conversation summarization — when session > 20 messages, summarize older turns
3. Tool result caching — `(tool_name, args_hash) → result`
4. W3C trace context / OpenTelemetry
5. OpenAPI / Swagger spec
6. `Config` struct to replace scattered hardcoded constants
7. Refactor Agent God object into composed sub-components
8. Real load testing (1000+ concurrent requests) + memory profiling

## Tests written but engineered

- `TestStream_QualityGateSwitchesToFallback` is engineered against specific tokenization. With real LLM streams, comma-bearing patterns ("i apologize, but as an") wouldn't match. Pass doesn't fully validate production behavior.
- `cache_test.go` async embedding test polls up to 1s — possibly flaky on slow CI.
