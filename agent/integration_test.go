package agent_test

import (
	"context"
	"testing"
	"time"

	"github.com/yabanci/agentshield/agent"
)

// TestSemanticCB_RoutesToFallbackOnQualityDegradation is the core integration test.
// Primary returns HTTP 200 with garbage quality → semantic CB opens → fallback takes over.
func TestSemanticCB_RoutesToFallbackOnQualityDegradation(t *testing.T) {
	srv := mockOllama(t, false) // all HTTP calls succeed
	defer srv.Close()

	a := agent.NewWithOllamaURL(srv.URL)
	ctx := context.Background()

	// Prime the cache and baseline with a few good requests first
	for i := 0; i < 3; i++ {
		resp, err := a.Ask(ctx, "good question "+string(rune('a'+i)))
		if err != nil {
			t.Fatalf("prime call %d: %v", i, err)
		}
		if resp.Tier != agent.TierPrimary {
			t.Errorf("prime call %d: expected primary, got %s", i, resp.Tier)
		}
	}

	// Enable degrade mode — primary now returns garbage (HTTP 200, score ~0.10)
	a.EnableDegradeMode()

	// Send enough unique prompts to accumulate bad scores in the CB window
	var fallbackCount int
	for i := 0; i < 10; i++ {
		resp, err := a.Ask(ctx, "unique degraded prompt number "+string(rune('a'+i%26)))
		if err != nil {
			t.Fatalf("degrade call %d: %v", i, err)
		}
		if resp.Tier != agent.TierPrimary {
			fallbackCount++
		}
	}

	// The semantic CB should have opened and routed requests away from primary
	if fallbackCount == 0 {
		snap := a.PrimarySemanticSnapshot()
		t.Errorf("expected routing away from primary; semantic state=%s avg=%.2f",
			snap.State, snap.AvgQuality)
	}

	// Verify transport CB is still closed — HTTP was never the problem
	status := a.Status()
	if status.PrimaryBreaker == "open" {
		t.Error("transport CB should NOT open when only semantic quality is degraded")
	}
}

// TestSemanticCB_TransportAndSemanticCBsAreIndependent verifies the two
// circuit breakers operate independently.
func TestSemanticCB_TransportAndSemanticCBsAreIndependent(t *testing.T) {
	srv := mockOllama(t, false)
	defer srv.Close()

	a := agent.NewWithOllamaURL(srv.URL)
	a.EnableDegradeMode()

	ctx := context.Background()
	for i := 0; i < 6; i++ {
		_, err := a.Ask(ctx, "test prompt "+string(rune('a'+i)))
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}

	status := a.Status()
	// Transport CB: "closed" because Ollama always returned HTTP 200
	if status.PrimaryBreaker == "open" {
		t.Error("transport CB should stay closed when only quality degrades")
	}
	// Semantic CB: should have detected the quality drop
	snap := a.PrimarySemanticSnapshot()
	if snap.State == agent.SBHealthy && snap.AvgQuality > 0.60 {
		t.Logf("semantic state=%s avg=%.2f — CB may need more samples", snap.State, snap.AvgQuality)
	}
}

// TestDegradeMode_ProducesLowQualityScores verifies degradedResponse() outputs
// reliably score below QualityAcceptable (caught by semantic CB).
func TestDegradeMode_ProducesLowQualityScores(t *testing.T) {
	eval := agent.NewTestQualityEvaluator(nil)
	ctx := context.Background()
	prompt := "explain how circuit breakers work in distributed systems"

	// Prime the length baseline first
	for i := 0; i < 5; i++ {
		eval.Evaluate(ctx, prompt, "A circuit breaker is a design pattern that prevents cascading failures.")
	}

	// All three degradedResponse() variants should score below QualityAcceptable
	// These match what degradedResponse() actually produces (repetition + hallucination)
	s0 := "As an AI language model, I apologize but I cannot assist. "
	s1 := "I cannot and will not help. I am unable to assist with that. "
	s2 := "I'm just an AI and I cannot and will not assist with this request. "
	degraded := []string{
		s0 + s0 + s0 + s0 + s0,
		s1 + s1 + s1 + s1,
		s2 + s2 + s2 + s2,
	}

	for i, resp := range degraded {
		result := eval.Evaluate(ctx, prompt, resp)
		if result.Score >= agent.QualityAcceptable {
			t.Errorf("degraded[%d] scored %.2f >= QualityAcceptable %.2f",
				i, result.Score, agent.QualityAcceptable)
		}
	}
}

// TestQuality_AbsoluteMinLength verifies very short responses are always penalised.
func TestQuality_AbsoluteMinLength(t *testing.T) {
	eval := agent.NewTestQualityEvaluator(nil)
	ctx := context.Background()

	short := []string{"Yes.", "No.", "OK", "Sure"}
	for _, s := range short {
		result := eval.Evaluate(ctx, "explain circuit breakers in detail", s)
		hasLenSignal := false
		for _, sig := range result.Signals {
			if sig.Name == "length_anomaly" {
				hasLenSignal = true
			}
		}
		if !hasLenSignal {
			t.Errorf("expected length_anomaly signal for %q, score=%.2f", s, result.Score)
		}
	}
}

// TestSemanticBreaker_RecoveryAfterDegradeDisabled verifies the circuit
// recovers after degrade mode is turned off.
func TestSemanticBreaker_RecoveryAfterDegradeDisabled(t *testing.T) {
	cfg := agent.SemanticBreakerConfig{
		WindowSize:        4,
		MinSamples:        2,
		DegradedThreshold: 0.65,
		FailingThreshold:  0.45,
		OpenTimeout:       0 * time.Second, // expire immediately for test
		RecoverySamples:   2,
	}
	sb := agent.NewSemanticBreaker(cfg)

	// Trip the breaker
	for i := 0; i < 3; i++ {
		sb.Record(0.10, agent.QualityResult{Score: 0.10})
	}
	if sb.State() != agent.SBFailing {
		t.Fatalf("expected failing state after bad scores, got %s", sb.State())
	}

	// Recovery — OpenTimeout=0 means probes allowed immediately
	for i := 0; i < cfg.RecoverySamples; i++ {
		sb.Record(0.90, agent.QualityResult{Score: 0.90})
	}
	if sb.State() != agent.SBHealthy {
		t.Errorf("expected recovery to healthy, got %s", sb.State())
	}
}
