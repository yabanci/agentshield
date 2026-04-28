package agent

import (
	"context"
	"fmt"
	"time"
)

// ChaosEvent is one event emitted during a chaos scenario.
type ChaosEvent struct {
	Type      string `json:"type"`       // "log" | "prompt" | "response" | "action" | "done"
	Message   string `json:"message"`
	Tier      string `json:"tier,omitempty"`
	Latency   int64  `json:"latency_ms,omitempty"`
	Timestamp int64  `json:"ts"`
}

// chaosPrompts are the prompts sent during the scenario.
// Using the same prompt at different stages exercises the cache tier.
var chaosPrompts = []string{
	"What is the circuit breaker pattern?",
	"Calculate 2^10 + 42",
	"What is the circuit breaker pattern?", // same → hits cache later
	"What time is it in UTC?",
}

// RunChaos executes the scripted failure scenario and sends events to ch.
// The caller must drain ch and close it when done.
func (a *Agent) RunChaos(ctx context.Context, ch chan<- ChaosEvent) {
	emit := func(t, msg string, tier Tier, latency int64) {
		select {
		case ch <- ChaosEvent{
			Type:      t,
			Message:   msg,
			Tier:      string(tier),
			Latency:   latency,
			Timestamp: time.Now().UnixMilli(),
		}:
		case <-ctx.Done():
		}
	}

	log := func(msg string) { emit("log", msg, "", 0) }
	send := func(prompt string) {
		emit("prompt", "→ "+prompt, "", 0)
		start := time.Now()
		resp, err := a.Ask(ctx, prompt)
		lat := time.Since(start).Milliseconds()
		if err != nil {
			emit("response", "✗ Error: "+err.Error(), TierDegraded, lat)
			return
		}
		tierEmoji := map[Tier]string{
			TierPrimary:  "🧠",
			TierFallback: "⚡",
			TierCache:    "💾",
			TierDegraded: "🔕",
		}
		emoji := tierEmoji[resp.Tier]
		cached := ""
		if resp.Cached {
			cached = " (cached)"
		}
		emit("response", fmt.Sprintf("← %s %s%s (%dms)", emoji, resp.Tier, cached, lat), resp.Tier, lat)
	}

	// ── Phase 0: baseline ──────────────────────────────────────────────
	log("🎬 Phase 1 — Baseline: all systems nominal")
	time.Sleep(500 * time.Millisecond)

	for _, p := range chaosPrompts[:2] {
		send(p)
		time.Sleep(300 * time.Millisecond)
	}

	// ── Phase 1: kill primary ──────────────────────────────────────────
	time.Sleep(800 * time.Millisecond)
	log("💀 Phase 2 — Killing primary model (llama3.2)...")
	emit("action", "kill_primary", "", 0)
	a.KillPrimary()
	time.Sleep(500 * time.Millisecond)

	log("📡 Sending requests — circuit breaker should route to fallback")
	for _, p := range chaosPrompts[:2] {
		send(p)
		time.Sleep(300 * time.Millisecond)
	}

	// ── Phase 2: kill fallback ─────────────────────────────────────────
	time.Sleep(800 * time.Millisecond)
	log("💀 Phase 3 — Killing fallback model (llama3.2:1b)...")
	emit("action", "kill_fallback", "", 0)
	a.KillFallback()
	time.Sleep(500 * time.Millisecond)

	log("📡 Both models down — sending cached and uncached prompts")
	// chaosPrompts[2] is same as [0] → should hit semantic cache
	send(chaosPrompts[2])
	time.Sleep(300 * time.Millisecond)
	// chaosPrompts[3] is new → graceful denial
	send(chaosPrompts[3])

	// ── Phase 3: restore primary ───────────────────────────────────────
	time.Sleep(800 * time.Millisecond)
	log("✅ Phase 4 — Restoring primary model...")
	emit("action", "restore_primary", "", 0)
	a.RestorePrimary()
	time.Sleep(500 * time.Millisecond)

	log("📡 Primary back — verifying auto-recovery")
	for _, p := range chaosPrompts[:2] {
		send(p)
		time.Sleep(300 * time.Millisecond)
	}

	// ── Phase 4: full restore ──────────────────────────────────────────
	time.Sleep(500 * time.Millisecond)
	emit("action", "restore_fallback", "", 0)
	a.RestoreFallback()
	log("✅ Phase 5 — All systems restored. Chaos demo complete.")
	emit("done", "Scenario finished", "", 0)
}
