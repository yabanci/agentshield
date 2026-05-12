package orchestrator_test

import (
	"testing"

	"github.com/yabanci/agentshield/orchestrator"
	"github.com/yabanci/agentshield/provider"
)

func TestChaos_KillRestorePrimary(t *testing.T) {
	c := orchestrator.NewChaos(nil)
	if c.IsPrimaryKilled() {
		t.Fatal("primary killed at construction")
	}
	c.KillPrimary()
	if !c.IsPrimaryKilled() {
		t.Fatal("KillPrimary did not flip flag")
	}
	c.RestorePrimary()
	if c.IsPrimaryKilled() {
		t.Fatal("RestorePrimary did not flip flag")
	}
}

func TestChaos_DegradeRoutesToWrapper(t *testing.T) {
	// Use a concrete DegradedWrapper around a stub LLMProvider via a separate Test
	// because LLMProvider needs a full interface implementation; here we just verify
	// that Enable/Disable on Chaos flips the wrapper's state.
	w := provider.NewDegradedWrapper(nil) // inner=nil OK for Enable/Disable check
	c := orchestrator.NewChaos(w)
	if c.IsDegradeEnabled() {
		t.Fatal("degrade enabled at construction")
	}
	c.EnableDegrade()
	if !w.IsEnabled() {
		t.Fatal("Chaos.EnableDegrade did not enable underlying wrapper")
	}
	if !c.IsDegradeEnabled() {
		t.Fatal("Chaos.IsDegradeEnabled false after enable")
	}
	c.DisableDegrade()
	if w.IsEnabled() {
		t.Fatal("Chaos.DisableDegrade did not disable wrapper")
	}
}

func TestChaos_NilDegradedWrapperIsNoOp(t *testing.T) {
	c := orchestrator.NewChaos(nil)
	c.EnableDegrade()
	if c.IsDegradeEnabled() {
		t.Fatal("nil wrapper should report not enabled")
	}
}

func TestChaos_TryStartIsExclusive(t *testing.T) {
	c := orchestrator.NewChaos(nil)
	if !c.TryStart() {
		t.Fatal("first TryStart should succeed")
	}
	if c.TryStart() {
		t.Fatal("second TryStart should fail while running")
	}
	c.Done()
	if !c.TryStart() {
		t.Fatal("TryStart after Done should succeed")
	}
}
