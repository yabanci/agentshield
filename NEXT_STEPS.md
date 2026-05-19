# NEXT_STEPS — AgentShield Hackathon Submission

**Hackathon**: [TrueFoundry Resilient Agents Challenge](https://devnetwork-ai-ml-hack-2026.devpost.com/) (DevNetwork AI+ML Hackathon 2026)
**Deadline**: 2026-05-28 @ 10:00 AM PDT → **22:00 Алматы (UTC+5)** on the same day
**Prize**: $1,500 cash, 2 winners
**Last refreshed**: 2026-05-19 (was 2026-04-30; that earlier snapshot is fully superseded)
**Status**: Engineering complete — ship-ready. `v0.2.0-hackathon-submission` tagged on `main`. 14 rounds of multi-agent audit closed (~125 findings). CI green. What's left is submission packaging (video, deploy, Devpost form).

Repo: https://github.com/yabanci/agentshield
Release: https://github.com/yabanci/agentshield/releases/tag/v0.2.0-hackathon-submission

---

## What's left to win (in priority order)

### 🔴 #1 — Demo video (1-2h, manual)

THE single biggest variable. Script is finalised at [`docs/demo-script.md`](docs/demo-script.md) (2:50 timing). Recording is on you — Kap / OBS / `ffmpeg avfoundation` all work on macOS. Suggested arc:

```
0:00  Dashboard at Score: 100. "AgentShield catches LLM failures that HTTP 200 hides."
0:15  Normal prompt → response → click trace icon → modal shows tier=primary, score=0.95
0:30  THE KILLER MOMENT: 🧪 Enable Degrade → send same prompt → response is gibberish
       Pause on dashboard: cb=closed ✅ but sb=failing ✗
       "Every other circuit breaker says 'all good'. AgentShield catches the lie."
1:00  ▶ Run Chaos Demo → quality sparkline drops 100 → 41 → 78 → 95 live
       Tier badge flips through primary → fallback → cache → denied
1:30  Switch to Streaming mode → quality-gated mid-stream
       Orange divider: "⚡ quality gate triggered, switched to fallback"
2:00  README + flowguard sister repo: "Powered by my own resilience library."
2:30  Bench numbers table: naive 0% useful vs AgentShield 100% on garbage
2:50  End card: GitHub URL + Devpost link
```

### 🔴 #2 — Live deployment (30min-2h)

`truefoundry/deploy.py` and `truefoundry/service.yaml` are ready. Full command-by-command runbook with both TrueFoundry and Fly.io paths lives in **[`docs/deploy-runbook.md`](docs/deploy-runbook.md)** — follow that. Plan to deploy on **2026-05-20** so there's a week-long buffer for surprises (auth, billing, image push, ingress, model availability).

### 🔴 #3 — Devpost submission form (15min copy-paste)

Full text drafted in [`docs/devpost-submission.md`](docs/devpost-submission.md). Paste into Devpost. Don't leave this for May 28 — submit at least 4h before deadline.

### 🟡 #4 — Dashboard screenshot in README (5min)

README has a placeholder near the top. Take a clean screenshot of the dashboard in "everything green" state (Score=100, all tiers shown) plus one in "everything degraded" state (Score~40, semantic CB failing). Drop into `docs/img/` and update README.

### 🟡 #5 — Chaos demo GIF (10min)

`ffmpeg -i recording.mov -vf "fps=12,scale=1200:-1" -loop 0 chaos-demo.gif`. Cut to the ~30s sparkline drop + tier badge flips. Embed under the README screenshot.

### 🟢 #6 — Pin repos on GitHub profile (1min)

Pin `agentshield` and `flowguard` (the sister resilience library) on your GitHub profile so the judges who click through see them first.

---

## What's already done since this doc was last touched (2026-04-30 → 2026-05-19)

Every Phase D item from the old NEXT_STEPS shipped. Don't waste time re-checking:

| Item | Where | PR |
|---|---|---|
| Multi-provider LLM (Ollama + OpenAI-compat: Groq, OpenRouter, vLLM, Mistral) | `provider/` | #13 |
| ReAct transcript summarization | `agent/summarize.go` | #21 |
| Tool result caching | `agent/toolcache.go` | #21 |
| OpenTelemetry distributed tracing | `telemetry/otel.go` + outbound `otelhttp` wrappers | #20 |
| `Config` struct replacing scattered constants | `config/config.go` | #11 |
| Agent god-object decomposed | Orchestrator extracted to `orchestrator/` | Round 7 |
| Bench vs naive integration (real numbers) | `bench/` | #19 |
| Grafana dashboard + Prometheus alerts | `deploy/` | #17 |
| `handler.go` integration tests (52.7% coverage) | `api/handler_test.go` | #18 |
| MCP integration (5th ReAct tool + standalone mock) | `agent/mcp_tool.go` + `cmd/mcp-mock/` | Round 8 |
| README polish + Resilience-score table sync | `README.md` | #16, #17, #20 |
| All endpoints documented + Configuration env-var table | `README.md` | #20 |

The "5 × 20pts Resilience Score" model is in the dashboard; the README is in sync.

---

## State of the codebase (current)

- **18 Go packages**, 13 with tests, all `-race -count=1` green
- **golangci-lint v2.11.4** with `forbidigo` rule: `os.Getenv` allowed ONLY in `config/` (exemption for `cmd/mcp-mock/`)
- **`toolchain go1.26.3`** pinned in `go.mod` (closes 4 stdlib CVEs incl. high-severity XSS bypass in `html/template`)
- **`Agent` struct** decomposed: Orchestrator owns the 4-tier degradation chain, Agent owns ReAct + tools + summarization + cache
- **18 env vars** consumed by `config/env.go` — full list in [`docs/deploy-runbook.md`](docs/deploy-runbook.md)
- **Auth defaults to OFF** for local dev; any shared deployment MUST set `AGENTSHIELD_AUTH_TOKEN`

---

## Hard rules to remember

- **Never push directly to `main`** — feature branch + PR always (per `CLAUDE.md`)
- **flowguard** is the sister dependency library (v0.3.0); agentshield consumes it. Both maintained separately. Two pinned repos on the GH profile reinforces the story.
- **Submit Devpost ≥ 4h before deadline** (2026-05-28 18:00 Алматы), not in the final hour.
- **Demo video records the live dashboard, not a mock-up.** If TrueFoundry deploy fails on May 27, fall back to local recording — better a local recording than no video.

---

## After the deadline (post-submission backlog)

These are intentionally NOT for the hackathon submission. Listed here so they don't get pulled in by accident:

1. OpenAPI / Swagger spec (currently only the dashboard documents endpoints visually)
2. Real load testing (1000+ concurrent requests) + memory profiling — `bench/` is a feature-correctness comparison, not a load test
3. Cost dashboard panel showing real-time $/request avoided
4. K8s native Helm chart (currently only Docker + TrueFoundry SDK)
5. Multi-region failover demo
6. Async embedding queue for the semantic cache (currently sync)
