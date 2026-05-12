// Package orchestrator owns the request-flow degradation chain
// (loadshed → bulkhead → primary → fallback → cache → graceful denial),
// the streaming quality gate, the breaker bundle, and chaos state flags.
package orchestrator

import (
	"sync/atomic"

	"github.com/yabanci/agentshield/provider"
)

// Chaos owns kill/restore/degrade atomic flags. Extracted from Agent so
// chaos state is not interleaved with operational state.
type Chaos struct {
	primaryKilled   atomic.Bool
	fallbackKilled  atomic.Bool
	chaosRunning    atomic.Bool
	degradedPrimary *provider.DegradedWrapper // may be nil in tests
}

// NewChaos wires Chaos around the DegradedWrapper used to inject low-quality
// primary responses during the chaos demo. Pass nil if no degraded wrapper.
func NewChaos(degradedPrimary *provider.DegradedWrapper) *Chaos {
	return &Chaos{degradedPrimary: degradedPrimary}
}

func (c *Chaos) KillPrimary()         { c.primaryKilled.Store(true) }
func (c *Chaos) RestorePrimary()      { c.primaryKilled.Store(false) }
func (c *Chaos) IsPrimaryKilled() bool { return c.primaryKilled.Load() }

func (c *Chaos) KillFallback()         { c.fallbackKilled.Store(true) }
func (c *Chaos) RestoreFallback()      { c.fallbackKilled.Store(false) }
func (c *Chaos) IsFallbackKilled() bool { return c.fallbackKilled.Load() }

func (c *Chaos) EnableDegrade() {
	if c.degradedPrimary != nil {
		c.degradedPrimary.Enable()
	}
}
func (c *Chaos) DisableDegrade() {
	if c.degradedPrimary != nil {
		c.degradedPrimary.Disable()
	}
}
func (c *Chaos) IsDegradeEnabled() bool {
	return c.degradedPrimary != nil && c.degradedPrimary.IsEnabled()
}

// TryStart marks a chaos scenario as running; returns false if one already runs.
func (c *Chaos) TryStart() bool { return c.chaosRunning.CompareAndSwap(false, true) }
func (c *Chaos) Done()          { c.chaosRunning.Store(false) }
func (c *Chaos) IsRunning() bool { return c.chaosRunning.Load() }
