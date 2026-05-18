# AgentShield — Demo Video Script

**Target:** Devpost submission for the [TrueFoundry Resilient Agents Challenge](https://devnetwork-ai-ml-hack-2026.devpost.com/)
**Length:** 2:00 ± 0:10 (Devpost sweet spot — judges watch many submissions)
**Format:** 1080p screen recording, system audio off (use VO), 30fps
**Tone:** confident senior engineer, not breathless marketer. Pause for emphasis. Let the dashboard do the talking.

---

## 0. Pre-production checklist (run once before first take)

```bash
# 1. Make sure ollama is up with llama3.2 + llama3.2:1b + nomic-embed-text pulled
ollama list
# Expected: llama3.2, llama3.2:1b, nomic-embed-text — all present

# 2. Clean build. For LOCAL recording: leave AGENTSHIELD_AUTH_TOKEN unset
#    so the dashboard is open. For a PUBLIC LIVE URL (Devpost link):
#    SET a token — otherwise any visitor can POST /demo/kill and wedge
#    the demo. Kill/Degrade actions auto-restore after 5 min as a safety
#    net but a token is still the right answer for shared deployments.
cd ~/Projects/pet/agentshield
unset AGENTSHIELD_AUTH_TOKEN          # local recording only
# export AGENTSHIELD_AUTH_TOKEN=$(openssl rand -hex 16)   # public deploy
go build -o /tmp/agentshield .
PORT=8080 OLLAMA_URL=http://localhost:11434 /tmp/agentshield

# 3. Open Chrome incognito, full-screen, zoom to 110%
#    (clean window, no extensions visible, dashboard fills the frame)
http://localhost:8080

# 4. Pin three browser tabs for cutaways:
#    Tab 1: dashboard (main)
#    Tab 2: https://github.com/yabanci/agentshield (code reveal)
#    Tab 3: https://github.com/yabanci/flowguard (the resilience library)

# 5. Recording tool: macOS screen recording (Cmd+Shift+5) or OBS at 1920x1080@30fps.
#    Audio: external mic, not built-in.

# 6. Pre-warm Ollama by sending one prompt before recording so the first response
#    in the take isn't slow due to model cold-start.
```

**Reset to score=100 before each take** (chaos state persists between takes):
- Click `✅ Restore Primary`, `✅ Restore Fallback`, `✅ Restore Quality` if any are red
- Wait 5s for sparkline to refill to 100
- Click anywhere to dismiss any open trace modal

---

## 1. Shooting strategy

Two viable approaches — pick one before recording:

**A. One-take (~2:30, recommended for first attempt)**
Record the whole thing live, narrate as you click. The off-the-cuff feel sells "this actually works." Allow one or two "uhm" moments — they read as authentic.

**B. Multi-take cut (more polished, ~3-4h work)**
Record each scene 2-3 times, pick the cleanest, hard-cut between them. Smoother but visibly produced — some judges weight this against you ("looks staged").

**Tip:** if going one-take, rehearse the click path 3 times silently before rolling. Most one-takes fail because the demo-er fumbles a button, not because the script is bad.

---

## 2. Scene-by-scene script

> **Conventions:**
> - **[VO]** = what you say
> - **[CAM]** = where the cursor / screen attention is
> - **[CUT]** = scene break
> - **[OST]** = on-screen text overlay (burn in during editing, 2s fade)

---

### Scene 1 — Cold open (0:00 → 0:12)

**[CAM]** Dashboard at Score: 100 / Grade A. Sparkline flat at top. Both badges green.

**[OST]** `AgentShield — semantic resilience for LLM agents`

**[VO]**
> "Traditional circuit breakers open when a service goes down. HTTP 5xx, timeouts, things you can measure.
>
> But LLMs don't fail that way. They stay up — and serve you garbage with a 200 OK."

*(Beat 1s. Cursor hovers near "Score: 100".)*

---

### Scene 2 — Normal request, the happy path (0:12 → 0:32)

**[CAM]** Click the prompt input. Type `What is Go in three sentences?` Hit Cmd+Enter.

*(Response streams in over ~3s. Show it.)*

**[CAM]** Click the `📋 trace` link in the resp-meta strip above the response box (or the `[trace↗]` link in the event log on the left).

**[OST]** `Trace modal → resilience timeline → tier path → quality score`

**[VO]**
> "Every response runs through a four-tier pipeline — primary, fallback, semantic cache, graceful denial — wrapped in load shedding, bulkheads, hedging, and circuit breakers.
>
> The trace modal opens with a horizontal **resilience timeline** — one bar per step, width proportional to its latency, colored by outcome. Every step is recorded, every response carries a trace ID."

*(Trace modal shows the flame timeline at top, then Primary tier → quality_score 0.94 → outcome success. Close the modal.)*

---

### Scene 3 — The killer moment (0:32 → 1:05)

**[CAM]** Cursor moves to `🧪 Enable Degrade` button. Click.

**[OST]** `🧪 degrade mode ON — primary now returns synthetic low-quality responses`

**[CAM]** Re-send the same prompt. The response is obvious garbage: *"As an AI language model, I apologize but I cannot assist. As an AI language model, I apologize but I cannot assist..."*

**[CAM]** Switch focus to the dashboard's degradation-chain card on the right. Point at the Primary row:
- `transport: closed` (green badge)
- `quality: OPEN` (red badge — the semantic CB just tripped)

**[OST]** `Transport: HEALTHY. Quality: BROKEN. Two independent breakers.`

**[VO]**
> "Watch this.
>
> The model responded — HTTP 200. The transport circuit breaker is closed. It thinks everything is fine.
>
> But the response is junk. AgentShield's **Semantic Circuit Breaker** caught it.
>
> Two breakers per model: one for the network, one for the *content*. Independent. Both have to be healthy."

*(Beat 1s. Cursor hovers between the two CB badges.)*

---

### Scene 4 — Side-by-side, the value-prop in one click (1:05 → 1:25)

**[CAM]** Type one more prompt: `What is a goroutine?`. Click `⚖️ Run Compare`.

*(Both columns populate ~3-5s later.)*

**[OST]** `Shielded vs raw — same prompt, parallel paths, one screen`

**[VO]**
> "Same prompt. Both paths fired in parallel. Left column: shielded — routed through the full chain. Right column: raw — direct LLM call, no AgentShield.
>
> During Degrade mode the raw side shows the garbage the model returned. The shielded side rerouted through fallback and got a real answer. Same model, same prompt — different outcomes."

*(Point at the quality_score badges — shielded shows ~90%, raw shows 18%.)*

---

### Scene 4b — Chaos demo, the sparkline (1:25 → 1:50)

**[CAM]** Click `✅ Restore Quality`. Click `▶ Run Chaos Demo`.

**[OST]** `▶ Chaos: baseline → kill primary → kill fallback → restore`

**[CAM]** **DON'T narrate over the next 15 seconds.** Watch the Score sparkline. It drops 100 → ~75 → ~41 as primary then fallback get killed, recovers through cache to ~78, then back to 95+.

**[VO] (after the score recovers)**
> "Score dropped to 41 and climbed back to 95 — under a minute. Each component — transport, quality, cache, availability, latency — heals independently. The composite is one number for the operator's glance."

**[CAM]** Switch to the **Charts** tab. Point at the live latency-per-tier histogram (p50 / p95 / p99) — primary bars towered during chaos, fallback bars stayed flat. Then point at the `Cost Savings` counter.

**[OST]** `Cost savings: $0.0034 → cache absorbed denied requests`

---

### Scene 5 — Quality-gated streaming (1:35 → 1:55)

**[CAM]** Click `📡 Stream` mode. Click `🧪 Enable Degrade` again.

**[CAM]** Send a streaming prompt: `Tell me about Go's garbage collector`.

**[CAM]** Tokens stream in. After ~30 tokens of garbage, an **orange horizontal divider** appears in the response with text *"⚡ quality gate triggered — switched to fallback (quality gate: matched: ...)"*. The rest of the response continues from the fallback model.

**[OST]** `Streaming quality gate — switches mid-response, no waiting for full output`

**[VO]**
> "Streaming makes this harder — you can't wait for the full response to evaluate it.
>
> So AgentShield checks every 30 tokens. The moment refusal markers appear in the stream, it cancels the primary mid-response and continues from the fallback. The user sees one continuous response."

---

### Scene 6 — Code reveal + close (1:55 → 2:10)

**[CUT]** Quick switch to Tab 2 (GitHub repo).

**[CAM]** Show the repo file tree briefly. Twelve domain packages: `agent`, `orchestrator`, `provider` (Ollama + OpenAI-compatible adapters), `quality`, `cache`, `telemetry`, `memory`, `config`, `api`, `api/web`, `internal/logctx`, `internal/logkeys`. Linger 2s.

**[VO] line-add — multi-provider**
> "And the chat backend is pluggable. The same resilience stack runs against Ollama locally, OpenAI, Groq, OpenRouter, vLLM — anything speaking the OpenAI chat-completions contract. One env var: `LLM_PROVIDER=openai`."

**[CUT]** Quick switch to Tab 3 (flowguard repo).

**[OST]** `flowguard — my own resilience library. Used here. Open source.`

**[VO]**
> "All of this is built on flowguard — a resilience library I wrote. Open source. Bulkhead, hedge, loadshed, circuit breaker.
>
> AgentShield adds the semantic layer on top.
>
> Production-grade Go, eleven packages, ninety-nine percent test coverage on the hot path. Zero external service dependencies for the resilience logic. Drop-in middleware."

**[CUT]** End card (static, 4s):

```
                AgentShield
   The first circuit breaker that understands LLM quality

           github.com/yabanci/agentshield
                                    
       TrueFoundry Resilient Agents — 2026
```

---

## 3. Captions / accessibility burn-ins

Burn these as on-screen captions during the corresponding scenes (use editing software's text track, sans-serif white-on-shadow, bottom-third):

| Time | Caption |
|---|---|
| 0:00 | Traditional CBs open on 5xx. LLMs fail with 200 OK. |
| 0:35 | HTTP 200 ≠ valid response |
| 0:50 | Transport CB: closed. Semantic CB: failing. |
| 1:10 | Chaos: primary down → fallback → recovery |
| 1:38 | Stream: 30-token quality probe, mid-response switch |
| 2:05 | flowguard.io — resilience primitives |

---

## 4. 60-second short cut (for Twitter/X / Devpost gallery thumbnail)

If you need a shorter version for thumbnail-clickbait:

- 0:00–0:08 — Scene 1 cold open (Score 100, problem statement)
- 0:08–0:35 — Scene 3 killer moment (degrade + dashboard split)
- 0:35–0:50 — Scene 4 chaos sparkline (no VO, just music + counter)
- 0:50–0:60 — End card

Same VO lines, just compressed Scenes 2 and 5 out. The killer moment carries the whole pitch.

---

## 5. Voice / pacing notes

- **Pause after "200 OK"** — let the absurdity land.
- **Don't read the dashboard** — point at it. Let viewers' eyes follow your cursor.
- **The 10s silence during chaos sparkline is intentional.** It's the most cinematic moment. Don't fill it with words.
- **The word "quality" should be emphasized every time** — it's the unique angle nobody else has.

Avoid:
- "So basically..." (drops authority)
- "As you can see..." (insulting)
- Listing tech ("we use Go, Prometheus, Ollama, ...") — judges don't care about stack, they care about insight.

---

## 6. Post-production checklist

- [ ] Trim head/tail silence (first frame should be Score: 100 already on screen)
- [ ] Add 0.5s fade-in / fade-out
- [ ] Color: bump contrast +5, saturation +10 (dashboard reads better on small mobile screens)
- [ ] Background music: instrumental, sub-bass roll under VO, fade out before the killer moment, return after end card. Recommended: anything from Epidemic Sound "tech reveal" tag at -22 dB.
- [ ] Export: 1080p H.264, ≤90 MB (Devpost upload cap), include captions baked in.
- [ ] Upload to YouTube unlisted as backup, embed YouTube link in Devpost (more reliable than direct upload).

---

## 7. Expected risks during shoot

| Risk | Mitigation |
|---|---|
| First prompt cold-starts Ollama (~6s delay) | Pre-warm before recording (see §0 step 6) |
| Chaos scenario produces a different sparkline than the 100→41→78→95 example | Re-record Scene 4 — it's about the *movement*, not the exact numbers |
| Quality gate doesn't trip in streaming because tokenization varies | Re-send the streaming prompt 2-3 times; the gate is probabilistic on hallucination markers |
| Trace modal closes too fast | Editing: freeze-frame the modal contents for 1.5s extra |
| `Run Chaos Demo` button already pressed (button shows "Running…") | Wait for it to finish (~25s), or restart the binary |

---

## 8. The 30-second elevator-pitch script (Devpost tagline section)

> "Traditional circuit breakers catch HTTP failures. LLMs fail differently — they stay up while serving garbage. AgentShield is the first resilience middleware with a Semantic Circuit Breaker that opens on quality degradation, not just transport errors. Two breakers per model, independent. Five-component Resilience Score. Quality-gated streaming. Production-grade Go, drop-in middleware."

(110 words. Cut to 80 if Devpost has a tagline word limit.)
