# Security Policy

## Posture

AgentShield is built for a hackathon track that specifically asks "how does
your agent behave when things go wrong." Security is part of that
question, so the project has been put through **fourteen rounds of
multi-agent audit** focused on different threat surfaces, not just
correctness. The full audit reports live in `docs/superpowers/` and the
working notes are in `/Users/arsenozhetov/.claude/jobs/` during
development.

## Threats addressed in the codebase today

| Vector | Status |
|---|---|
| Reflected/stored XSS in the dashboard | DOM-API rendering everywhere; CSP + SRI on the chart.js CDN script; `html/template` escape relied on |
| Bearer-token auth bypass on `/demo/*`, `/sessions/*`, `/trace/{id}`, `/config/webhook` | `requireAuth` middleware + constant-time comparison; falls open by default for local dev, gated when `AGENTSHIELD_AUTH_TOKEN` is set |
| Server-side request forgery via webhook URL | URL allow-listing (http / https only by default), hostname → IP resolution check against private/loopback ranges, `CheckRedirect` denies cross-origin 30x to internal IPs |
| Public live URL kill switch (`/demo/kill` loop) | Per-IP rate limit (10/min sliding window) on every `/demo/*` endpoint, plus a non-resetting 5-minute auto-restore timer (with generation-counter handshake) so an attacker cannot extend the deadline by repeating the call |
| Trace-prompt PII exfiltration | When auth is disabled, the `Prompt` field is scrubbed from `/trace/{id}` responses |
| Calibration poisoning of the semantic CB | Floor on σ + ceiling clamps on the learned `degraded`/`failing` thresholds so an adversary cannot tighten the band by pre-seeding high-quality samples |
| Webhook-driven goroutine fan-out | 32-slot semaphore + dropped-event metric + throttled WARN log so a chaos burst cannot mask real CB transitions |
| Stdlib CVEs reachable from the dashboard `html/template` path | `toolchain go1.26.3` pinned in `go.mod` (closes GO-2026-4980 high-severity XSS bypass plus three medium-severity vulns) |
| MCP mock server kill switch reachable from non-loopback | Defaults bind to `127.0.0.1`; non-loopback binds require explicit `MCP_MOCK_BIND` and log a WARN |
| Trusted-proxy spoofing via `X-Forwarded-For` | `AGENTSHIELD_TRUSTED_PROXIES` allow-list; header is dropped when the peer is not in the list |

## Reporting a vulnerability

For now this is a hackathon submission, not a deployed service. Open a
GitHub issue or email the address on the repo owner's profile. If the
project survives the hackathon and grows users, this section will be
replaced with a real coordinated-disclosure policy.

## OTel tracing and PII

Tool inputs are captured as OTel span attributes (truncated to 2 KB). See
[`docs/security-considerations.md`](docs/security-considerations.md) for the
full trade-off analysis and mitigation options.

## Audit reports

The fourteen round-by-round audit reports are in `/Users/arsenozhetov/.claude/jobs/` and a high-level summary lives in the repo's commit history — each round closes with a `fix: round-N — …` commit listing the findings addressed.
