// Package orchestrator owns the request-flow degradation chain
// (loadshed → bulkhead → primary → fallback → cache → graceful denial),
// the streaming quality gate, the breaker bundle, and chaos state flags.
package orchestrator

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/yabanci/agentshield/provider"
)

// chaosAutoRestoreTimeout is how long after a kill/degrade we
// automatically restore. Demo segments take <60s; 5 min is well above
// any legitimate demo timing yet caps the blast radius if a malicious
// visitor on a public live URL triggers /demo/kill — they can interrupt
// for at most this long before state self-heals.
const chaosAutoRestoreTimeout = 5 * time.Minute

// Chaos owns kill/restore/degrade atomic flags. Extracted from Agent so
// chaos state is not interleaved with operational state.
type Chaos struct {
	primaryKilled   atomic.Bool
	fallbackKilled  atomic.Bool
	chaosRunning    atomic.Bool
	degradedPrimary *provider.DegradedWrapper // may be nil in tests

	mu sync.Mutex
	// One timer slot + generation counter per kind. The generation is
	// bumped on every cancel so an AfterFunc callback that was already
	// queued at cancel time can self-detect as superseded and no-op.
	// Without this, RestorePrimary→KillPrimary in quick succession could
	// let the OLD timer's restore-callback fire AFTER the new kill and
	// undo it.
	primTimer    *time.Timer
	primGen      uint64
	fallTimer    *time.Timer
	fallGen      uint64
	degradeTimer *time.Timer
	degradeGen   uint64
}

// NewChaos wires Chaos around the DegradedWrapper used to inject low-quality
// primary responses during the chaos demo. Pass nil if no degraded wrapper.
func NewChaos(degradedPrimary *provider.DegradedWrapper) *Chaos {
	return &Chaos{degradedPrimary: degradedPrimary}
}

// scheduleAutoRestore arms a NON-RESETTING timer that fires `restore`
// after chaosAutoRestoreTimeout. If a timer is already pending for this
// slot, the call is a no-op — this is the critical safety property: an
// attacker spamming /demo/kill in a loop cannot keep state poisoned past
// the original timer's deadline by re-arming it. The timer clears its
// own slot when it fires; a future kill (after the restore) can arm a
// fresh one.
//
// Generation handshake: every scheduling captures the current `*gen`. If
// `cancelTimer` ran between schedule and callback, `*gen` was bumped,
// and the callback compares its captured value, finds a mismatch, and
// no-ops. This closes the race where Restore→Kill ordering would let an
// in-flight original callback fire after the new kill and undo it.
func (c *Chaos) scheduleAutoRestore(slot **time.Timer, gen *uint64, restore func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if *slot != nil {
		return // pending — keep the original deadline
	}
	myGen := *gen
	*slot = time.AfterFunc(chaosAutoRestoreTimeout, func() {
		c.mu.Lock()
		if *gen != myGen {
			// Superseded — explicit Restore* + new Kill* happened while
			// we were queued. The new schedule (if any) owns the slot;
			// our restore call would undo the new kill, so skip it.
			c.mu.Unlock()
			return
		}
		*slot = nil
		c.mu.Unlock()
		restore()
	})
}

func (c *Chaos) cancelTimer(slot **time.Timer, gen *uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if *slot != nil {
		(*slot).Stop()
		*slot = nil
	}
	*gen++ // invalidate any AfterFunc callback already queued
}

func (c *Chaos) KillPrimary() {
	c.primaryKilled.Store(true)
	c.scheduleAutoRestore(&c.primTimer, &c.primGen, func() { c.primaryKilled.Store(false) })
}
func (c *Chaos) RestorePrimary() {
	c.primaryKilled.Store(false)
	c.cancelTimer(&c.primTimer, &c.primGen)
}
func (c *Chaos) IsPrimaryKilled() bool { return c.primaryKilled.Load() }

func (c *Chaos) KillFallback() {
	c.fallbackKilled.Store(true)
	c.scheduleAutoRestore(&c.fallTimer, &c.fallGen, func() { c.fallbackKilled.Store(false) })
}
func (c *Chaos) RestoreFallback() {
	c.fallbackKilled.Store(false)
	c.cancelTimer(&c.fallTimer, &c.fallGen)
}
func (c *Chaos) IsFallbackKilled() bool { return c.fallbackKilled.Load() }

func (c *Chaos) EnableDegrade() {
	if c.degradedPrimary == nil {
		return
	}
	c.degradedPrimary.Enable()
	c.scheduleAutoRestore(&c.degradeTimer, &c.degradeGen, func() {
		if c.degradedPrimary != nil {
			c.degradedPrimary.Disable()
		}
	})
}
func (c *Chaos) DisableDegrade() {
	if c.degradedPrimary != nil {
		c.degradedPrimary.Disable()
	}
	c.cancelTimer(&c.degradeTimer, &c.degradeGen)
}
func (c *Chaos) IsDegradeEnabled() bool {
	return c.degradedPrimary != nil && c.degradedPrimary.IsEnabled()
}

// TryStart marks a chaos scenario as running; returns false if one already runs.
func (c *Chaos) TryStart() bool  { return c.chaosRunning.CompareAndSwap(false, true) }
func (c *Chaos) Done()           { c.chaosRunning.Store(false) }
func (c *Chaos) IsRunning() bool { return c.chaosRunning.Load() }
