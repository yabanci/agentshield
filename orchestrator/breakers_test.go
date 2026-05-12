package orchestrator_test

import (
	"testing"

	"github.com/yabanci/flowguard/circuitbreaker"

	"github.com/yabanci/agentshield/orchestrator"
	"github.com/yabanci/agentshield/quality"
)

func TestBreakerSet_BundlesAllFour(t *testing.T) {
	pt := circuitbreaker.NewAdaptive(20, 0.5, 5)
	ft := circuitbreaker.New(circuitbreaker.WithFailureThreshold(3))
	ps := quality.NewSemanticBreaker(quality.DefaultSBConfig)
	fs := quality.NewSemanticBreaker(quality.DefaultSBConfig)

	bs := orchestrator.NewBreakerSet(pt, ft, ps, fs)
	if bs.PrimaryTransport != pt || bs.FallbackTransport != ft {
		t.Errorf("transport breakers not bundled correctly")
	}
	if bs.PrimarySemantic != ps || bs.FallbackSemantic != fs {
		t.Errorf("semantic breakers not bundled correctly")
	}
	if bs.PrimaryTransport.State().String() != "closed" {
		t.Errorf("PrimaryTransport should start closed, got %s", bs.PrimaryTransport.State())
	}
}
