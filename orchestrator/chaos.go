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
//
// Auto-restore: every Kill*/EnableDegrade schedules a goroutine that
// reverses the action after chaosAutoRestoreTimeout. An explicit
// Restore*/DisableDegrade cancels the pending timer (so the auto-restore
// is a safety net, not a forced behavior on the presenter).
type Chaos struct {
	primaryKilled   atomic.Bool
	fallbackKilled  atomic.Bool
	chaosRunning    atomic.Bool
	degradedPrimary *provider.DegradedWrapper // may be nil in tests

	mu           sync.Mutex
	primTimer    *time.Timer
	fallTimer    *time.Timer
	degradeTimer *time.Timer
}

// NewChaos wires Chaos around the DegradedWrapper used to inject low-quality
// primary responses during the chaos demo. Pass nil if no degraded wrapper.
func NewChaos(degradedPrimary *provider.DegradedWrapper) *Chaos {
	return &Chaos{degradedPrimary: degradedPrimary}
}

// scheduleAutoRestore swaps the named timer for a fresh one that fires
// `restore` after chaosAutoRestoreTimeout. Calling Kill* twice resets
// the clock; Restore* cancels.
func (c *Chaos) scheduleAutoRestore(slot **time.Timer, restore func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if *slot != nil {
		(*slot).Stop()
	}
	*slot = time.AfterFunc(chaosAutoRestoreTimeout, restore)
}

func (c *Chaos) cancelTimer(slot **time.Timer) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if *slot != nil {
		(*slot).Stop()
		*slot = nil
	}
}

func (c *Chaos) KillPrimary() {
	c.primaryKilled.Store(true)
	c.scheduleAutoRestore(&c.primTimer, func() { c.primaryKilled.Store(false) })
}
func (c *Chaos) RestorePrimary() {
	c.primaryKilled.Store(false)
	c.cancelTimer(&c.primTimer)
}
func (c *Chaos) IsPrimaryKilled() bool { return c.primaryKilled.Load() }

func (c *Chaos) KillFallback() {
	c.fallbackKilled.Store(true)
	c.scheduleAutoRestore(&c.fallTimer, func() { c.fallbackKilled.Store(false) })
}
func (c *Chaos) RestoreFallback() {
	c.fallbackKilled.Store(false)
	c.cancelTimer(&c.fallTimer)
}
func (c *Chaos) IsFallbackKilled() bool { return c.fallbackKilled.Load() }

func (c *Chaos) EnableDegrade() {
	if c.degradedPrimary == nil {
		return
	}
	c.degradedPrimary.Enable()
	c.scheduleAutoRestore(&c.degradeTimer, func() {
		if c.degradedPrimary != nil {
			c.degradedPrimary.Disable()
		}
	})
}
func (c *Chaos) DisableDegrade() {
	if c.degradedPrimary != nil {
		c.degradedPrimary.Disable()
	}
	c.cancelTimer(&c.degradeTimer)
}
func (c *Chaos) IsDegradeEnabled() bool {
	return c.degradedPrimary != nil && c.degradedPrimary.IsEnabled()
}

// TryStart marks a chaos scenario as running; returns false if one already runs.
func (c *Chaos) TryStart() bool   { return c.chaosRunning.CompareAndSwap(false, true) }
func (c *Chaos) Done()            { c.chaosRunning.Store(false) }
func (c *Chaos) IsRunning() bool  { return c.chaosRunning.Load() }
