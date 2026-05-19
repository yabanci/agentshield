# Deploy Runbook — AgentShield Live URL for Judges

**Goal:** stand up a publicly reachable URL judges can poke before
2026-05-28 22:00 Алматы (10:00 AM PDT, the Devpost cut-off).

**Recommended target:** TrueFoundry (sponsor of the track — extra story
points). **Backup:** Fly.io if TrueFoundry account/billing/limits trip.
**Last-resort:** local + ngrok tunnel for the recording session only.

> Plan to deploy by **2026-05-20** so there's a week buffer for surprises
> (auth, billing, image push, ingress, model availability).

---

## 0. Pre-flight (5 min, do once)

```bash
# Confirm you're on the tagged release commit
cd ~/projects/pet/agentshield
git checkout main
git pull --ff-only origin main
git describe --tags --exact-match  # must print: v0.2.0-hackathon-submission

# Make sure the image actually builds locally before paying for a remote build
docker build -t agentshield:local .
docker run --rm -p 8080:8080 \
  -e OLLAMA_URL=http://host.docker.internal:11434 \
  -e AGENTSHIELD_AUTH_TOKEN=local-smoke \
  --add-host=host.docker.internal:host-gateway \
  agentshield:local &
sleep 3
curl -fsS http://localhost:8080/health/live && echo OK
curl -fsS http://localhost:8080/health/ready  # may 503 if no Ollama — OK, just confirms wiring
docker stop $(docker ps -q --filter ancestor=agentshield:local)
```

If the local image doesn't start, fix that before going remote — no remote runtime will be easier to debug.

### Generate the auth token now (you'll paste it in 3 places)

```bash
# 32-byte hex token, stable across all the secret-create calls below
export AS_TOKEN="$(openssl rand -hex 32)"
echo "$AS_TOKEN"   # copy this — used for /demo/*, /sessions/*, /trace/{id}
```

Store in your password manager. Same token goes into the TrueFoundry/Fly secret AND your local recording session.

### Pick the LLM backend

The deploy below ships with two paths. **Use ONE of these for the demo:**

| Path | When | What to set |
|---|---|---|
| **A. OpenAI-compatible** (recommended for hackathon) | When you don't want to host Ollama+GPU on TrueFoundry | `LLM_PROVIDER=openai` + `OPENAI_API_KEY` + `OPENAI_BASE_URL` |
| B. TrueFoundry-hosted Ollama | If TrueFoundry's catalog has a free Ollama service in your workspace | `OLLAMA_URL=http://<internal-svc>:11434` |

Path A is cheaper and faster to set up. The OpenAI-compatible endpoint can be **Groq** (free tier, fast), **OpenRouter**, **Together**, or actual OpenAI. Groq is the easiest free option.

---

## 1. TrueFoundry path (preferred — sponsor's platform)

### 1.1 Install & login (one-time)

```bash
# Python venv keeps the SDK away from system python
python3 -m venv ~/.venvs/truefoundry
source ~/.venvs/truefoundry/bin/activate
pip install -U truefoundry

# Browser-based OAuth flow
tfy login
# Pick the workspace shown in the hackathon welcome email
# Note the Workspace FQN — looks like: tfy-cluster:my-workspace
export TFY_WORKSPACE="<paste-workspace-fqn-here>"
```

If `tfy login` doesn't open a browser on macOS, copy the URL it prints into Safari/Chrome manually.

### 1.2 Create secrets (one-time)

```bash
# Auth token from step 0
tfy secret create --name AGENTSHIELD_AUTH_TOKEN --value "$AS_TOKEN"

# Path A — OpenAI-compatible (e.g. Groq):
tfy secret create --name OPENAI_API_KEY --value "gsk_..."   # your Groq/OpenAI/Together key

# Path B — only if you host Ollama on TrueFoundry:
# tfy secret create --name OLLAMA_URL --value "http://ollama.<workspace>.svc.cluster.local:11434"
```

Verify they exist:

```bash
tfy secret list | grep -E '(AGENTSHIELD_AUTH_TOKEN|OPENAI_API_KEY|OLLAMA_URL)'
```

### 1.3 Configure provider in `deploy.py`

Open `truefoundry/deploy.py` and set the provider env section. For **Groq** path A, change the env block to:

```python
env={
    "PORT": "8080",
    "LLM_PROVIDER": "openai",
    "OPENAI_BASE_URL": "https://api.groq.com/openai/v1",
    "OPENAI_PRIMARY_MODEL": "llama-3.3-70b-versatile",
    "OPENAI_FALLBACK_MODEL": "llama-3.1-8b-instant",
    "OPENAI_EMBED_MODEL": "",   # Groq doesn't serve embeddings; semantic cache will skip
    "OPENAI_API_KEY": StringDataOrSecretRef(
        value_from=SecretRef(secret_fqn="secret:OPENAI_API_KEY")
    ),
    "AGENTSHIELD_AUTH_TOKEN": StringDataOrSecretRef(
        value_from=SecretRef(secret_fqn="secret:AGENTSHIELD_AUTH_TOKEN")
    ),
    # OLLAMA_URL stays as a secret ref but won't be used when LLM_PROVIDER=openai;
    # set to a sentinel so the Config validation passes.
    "OLLAMA_URL": "http://unused:11434",
},
```

For **OpenRouter** swap `OPENAI_BASE_URL` to `https://openrouter.ai/api/v1` and use any model FQN from their catalog. For **actual OpenAI** leave defaults and embed model defaults work.

> **Why no Ollama?** Path A uses Groq's hosted inference — fast, free tier, no GPU on TrueFoundry needed. If you want Ollama, swap to path B (deploy Ollama as a second Service in the same workspace, then point `OLLAMA_URL` at it).

### 1.4 Deploy

```bash
cd ~/projects/pet/agentshield
source ~/.venvs/truefoundry/bin/activate   # if not still active
python truefoundry/deploy.py --workspace "$TFY_WORKSPACE"
```

This:
- triggers a remote Docker build from the repo's `Dockerfile`
- pushes to TrueFoundry's image registry
- creates the Service with the env/secret bindings
- exposes it on `https://agentshield-<workspace>.<cluster>.truefoundry.app/`

Tail logs while it deploys:

```bash
tfy logs --service agentshield --workspace "$TFY_WORKSPACE" --follow
```

### 1.5 Verify the live URL

```bash
export LIVE_URL="https://agentshield-<your-subdomain>.truefoundry.app"
# Replace ^^ with the URL printed by deploy or visible in the TrueFoundry dashboard

# Liveness — no dep check
curl -fsS "$LIVE_URL/health/live"

# Readiness — exercises the LLM provider; first call may be slow while Groq warms up
curl -fsS "$LIVE_URL/health/ready"

# Metrics endpoint should not 401 (unauthenticated)
curl -fsS "$LIVE_URL/metrics" | head -20

# Auth gate — must 401 without the token
curl -i -X POST "$LIVE_URL/demo/kill"
# Expect: HTTP/1.1 401

# Auth gate — must 200 with the token
curl -i -X POST "$LIVE_URL/demo/kill" -H "Authorization: Bearer $AS_TOKEN"
# Expect: HTTP/1.1 200

# Restore the primary so subsequent chats work
curl -fsS -X POST "$LIVE_URL/demo/restore" -H "Authorization: Bearer $AS_TOKEN"

# Smoke test the chat endpoint
curl -fsS -X POST "$LIVE_URL/chat" \
  -H "Content-Type: application/json" \
  -d '{"message": "what is 2+2?"}'
```

If chat returns a sensible answer, you're done.

### 1.6 Capture the URL for Devpost + recording

```bash
echo "$LIVE_URL" > ~/.agentshield-live-url
# Paste $LIVE_URL into:
#   - Devpost submission form: "Try it" field
#   - README.md: replace the "Live demo: TBD" placeholder if present
#   - docs/devpost-submission.md
```

---

## 2. Fly.io path (backup — if TrueFoundry fails)

Fly.io is well-tested for Go services and gives a free `*.fly.dev` subdomain.

```bash
# Install flyctl (Homebrew or curl)
brew install flyctl
# or: curl -L https://fly.io/install.sh | sh

# One-time signup/login (browser flow)
flyctl auth login

# Initialise the app — answer NO when it asks "Deploy now?"
cd ~/projects/pet/agentshield
flyctl launch --no-deploy --name agentshield --region sjc
# This writes fly.toml — edit it before deploying:
#   [http_service] internal_port = 8080
#   [http_service.checks] path = "/health/live"
```

Then set secrets and deploy:

```bash
flyctl secrets set \
  AGENTSHIELD_AUTH_TOKEN="$AS_TOKEN" \
  LLM_PROVIDER="openai" \
  OPENAI_BASE_URL="https://api.groq.com/openai/v1" \
  OPENAI_API_KEY="gsk_..." \
  OPENAI_PRIMARY_MODEL="llama-3.3-70b-versatile" \
  OPENAI_FALLBACK_MODEL="llama-3.1-8b-instant" \
  OLLAMA_URL="http://unused:11434"

flyctl deploy

# URL appears in the output, e.g. https://agentshield.fly.dev
export LIVE_URL="https://agentshield.fly.dev"

# Same smoke test as step 1.5
flyctl logs --app agentshield   # tail logs while testing
```

Fly's free tier is sufficient for a demo. If memory is tight, bump in `fly.toml`:

```toml
[[vm]]
  memory_mb = 512
  cpu_kind = "shared"
  cpus = 1
```

---

## 3. Last-resort: local + ngrok (for the recording only)

Only use this if BOTH TrueFoundry and Fly fail on demo day. Recording works, but judges can't poke a dead URL after — so submit before midnight on May 27 if going this route.

```bash
# Terminal 1 — run the binary
cd ~/projects/pet/agentshield
AGENTSHIELD_AUTH_TOKEN="$AS_TOKEN" \
LLM_PROVIDER=openai \
OPENAI_BASE_URL=https://api.groq.com/openai/v1 \
OPENAI_API_KEY=gsk_... \
OPENAI_PRIMARY_MODEL=llama-3.3-70b-versatile \
OPENAI_FALLBACK_MODEL=llama-3.1-8b-instant \
OLLAMA_URL=http://unused:11434 \
go run .

# Terminal 2 — expose via ngrok (free tier OK)
ngrok http 8080
# Note the https://*.ngrok-free.app URL it prints
```

This is fine for the **recording**. Do NOT submit this URL to Devpost — ngrok tunnels die when your laptop sleeps.

---

## 4. Reference: full env-var inventory

All env vars `config/env.go` reads. **Bold = required for any meaningful deploy.**

| Var | Default | Purpose |
|---|---|---|
| `PORT` | `8080` | HTTP listen port |
| **`OLLAMA_URL`** | `http://localhost:11434` | Ollama backend; set to a sentinel like `http://unused:11434` when `LLM_PROVIDER=openai` |
| `LLM_PROVIDER` | `ollama` | `ollama` or `openai` (OpenAI-compatible adapter) |
| `OPENAI_BASE_URL` | `https://api.openai.com/v1` | OpenAI-compatible endpoint root |
| `OPENAI_API_KEY` | — | Required when `LLM_PROVIDER=openai` |
| `OPENAI_PRIMARY_MODEL` | `gpt-4o-mini` | Primary tier model name |
| `OPENAI_FALLBACK_MODEL` | `gpt-4o-mini` | Fallback tier model name |
| `OPENAI_EMBED_MODEL` | empty | Empty = semantic cache cosine-sim falls back to exact match |
| **`AGENTSHIELD_AUTH_TOKEN`** | empty | If set, gates `/demo/*`, `/sessions/*`, `/trace/{id}`, `/config/webhook`. **Required for any public deploy.** |
| `AGENTSHIELD_ALLOW_HTTP_WEBHOOK` | `false` | Permit `http://` webhooks (dev only) |
| `AGENTSHIELD_ALLOW_PRIVATE_WEBHOOK` | `false` | Permit webhooks pointing at RFC1918/loopback (dev only) |
| `AGENTSHIELD_TRUSTED_PROXIES` | empty | CIDR allowlist whose `X-Forwarded-For` header is trusted |
| `AGENTSHIELD_TOOL_CACHE_ENABLED` | `true` | Per-session ReAct tool result cache |
| `AGENTSHIELD_TOOL_CACHE_MAX_ENTRIES` | `64` | LRU cap per session |
| `AGENTSHIELD_REACT_MAX_TRANSCRIPT_TOKENS` | `6000` | Threshold above which transcript summarization kicks in |
| `MCP_URL` | empty | If set, the 5th ReAct tool `mcp_lookup` POSTs here |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | empty | OTLP/gRPC endpoint; empty = no-op tracer |
| `OTEL_EXPORTER_OTLP_INSECURE` | `true` | **Set to `false` in production** — `true` means plaintext OTLP |
| `OTEL_EXPORTER_OTLP_TIMEOUT` | `10s` | Export timeout |
| `LOG_LEVEL` | `info` | `debug` / `info` / `warn` / `error` |
| `LOG_FORMAT` | `text` | `text` or `json` |

---

## 5. Rollback / teardown

### TrueFoundry

```bash
# Pause the service (keeps spec, stops billing)
tfy service scale --name agentshield --workspace "$TFY_WORKSPACE" --replicas 0

# Or fully remove
tfy service delete --name agentshield --workspace "$TFY_WORKSPACE"
```

### Fly.io

```bash
flyctl scale count 0 --app agentshield   # pause, free
flyctl apps destroy agentshield          # full removal
```

### Local + ngrok

`Ctrl+C` both terminals.

---

## 6. Troubleshooting

**`/health/ready` returns 503 forever** → the LLM provider can't be reached. Check `OPENAI_BASE_URL` is right and the key is valid. Tail logs.

**`curl /chat` returns "shielded call failed" with no upstream detail** → that's intentional — error scrubbing strips the upstream URL. Check the server logs for the real error.

**`/demo/kill` returns 200 but the dashboard doesn't show "denied"** → that's correct; the dashboard polls `/status` every 2s. Wait for next refresh or check the WebSocket/SSE if streaming is enabled.

**OTel spans not showing in your collector** → confirm `OTEL_EXPORTER_OTLP_INSECURE=true` if your collector accepts plaintext gRPC; check the network path from the runtime to the collector.

**TrueFoundry build fails on `go mod download`** → the workspace may not have outbound internet for proxy.golang.org. Add `GOPROXY=direct` env or vendor deps with `go mod vendor` before deploy.

**Fly.io: "Error: We need permission to allocate IPs"** → run `flyctl ips allocate-v4 --shared` then redeploy. The shared v4 is free on Fly's hobby tier.

---

## 7. Done-criteria checklist

Before declaring deploy done:

- [ ] `$LIVE_URL/health/live` returns 200
- [ ] `$LIVE_URL/health/ready` returns 200 (after first chat warms the model)
- [ ] `$LIVE_URL/metrics` returns Prometheus text format, NOT a 401
- [ ] `$LIVE_URL/demo/kill` returns 401 without `Authorization`, 200 with it
- [ ] `$LIVE_URL/chat` returns a real LLM response for "what is 2+2?"
- [ ] The dashboard at `$LIVE_URL/` loads and shows Score=100
- [ ] You can reproduce the killer demo (enable degrade → response is gibberish → CB metrics update)
- [ ] `$LIVE_URL` saved to `~/.agentshield-live-url` and pasted into Devpost draft
- [ ] At least one judge-style smoke run from a phone (different network) reaches the dashboard
