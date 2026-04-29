package agent_test

import (
	"testing"
	"time"

	"github.com/yabanci/agentshield/agent"
)

func testCfg() agent.SemanticBreakerConfig {
	return agent.SemanticBreakerConfig{
		WindowSize:        6,
		MinSamples:        3,
		DegradedThreshold: 0.65,
		FailingThreshold:  0.45,
		OpenTimeout:       60 * time.Second, // won't expire in test
		RecoverySamples:   2,
	}
}

func record(sb *agent.SemanticBreaker, score float64) {
	sb.Record(score, agent.QualityResult{Score: score})
}

func TestSB_StartsHealthy(t *testing.T) {
	sb := agent.NewSemanticBreaker(testCfg())
	if sb.State() != agent.SBHealthy {
		t.Errorf("expected healthy, got %s", sb.State())
	}
	if sb.ShouldBlock() {
		t.Error("should not block initially")
	}
}

func TestSB_StaysHealthyOnGoodScores(t *testing.T) {
	sb := agent.NewSemanticBreaker(testCfg())
	for i := 0; i < 6; i++ {
		record(sb, 0.90)
	}
	if sb.State() != agent.SBHealthy {
		t.Errorf("expected healthy, got %s", sb.State())
	}
}

func TestSB_TransitionsToDegradedOnMediumScores(t *testing.T) {
	sb := agent.NewSemanticBreaker(testCfg())
	for i := 0; i < 4; i++ {
		record(sb, 0.50) // below degraded threshold (0.65) but above failing (0.45)
	}
	if sb.State() == agent.SBHealthy {
		t.Error("expected degraded or failing, still healthy")
	}
}

func TestSB_OpensOnBadScores(t *testing.T) {
	sb := agent.NewSemanticBreaker(testCfg())
	for i := 0; i < 4; i++ {
		record(sb, 0.20) // well below failing threshold
	}
	if sb.State() != agent.SBFailing {
		t.Errorf("expected failing, got %s", sb.State())
	}
	if !sb.ShouldBlock() {
		t.Error("should block when failing")
	}
}

func TestSB_TripReason_SetWhenFailing(t *testing.T) {
	sb := agent.NewSemanticBreaker(testCfg())
	for i := 0; i < 4; i++ {
		record(sb, 0.10)
	}
	snap := sb.Snapshot()
	if snap.TripReason == "" {
		t.Error("expected non-empty trip reason when failing")
	}
}

func TestSB_RecoveryAfterGoodScores(t *testing.T) {
	cfg := testCfg()
	cfg.OpenTimeout = 0 * time.Second // expire immediately so probe is allowed
	sb := agent.NewSemanticBreaker(cfg)

	// Trip the breaker
	for i := 0; i < 4; i++ {
		record(sb, 0.10)
	}
	if sb.State() != agent.SBFailing {
		t.Fatal("expected failing state")
	}

	// Recovery — enough good scores
	for i := 0; i < cfg.RecoverySamples; i++ {
		record(sb, 0.90)
	}
	if sb.State() != agent.SBHealthy {
		t.Errorf("expected recovery to healthy, got %s", sb.State())
	}
}

func TestSB_RollingAvg_AccuracyWithWindow(t *testing.T) {
	sb := agent.NewSemanticBreaker(testCfg())
	// Add 3 good, then 3 bad — avg should be ~0.5
	for i := 0; i < 3; i++ {
		record(sb, 1.0)
	}
	for i := 0; i < 3; i++ {
		record(sb, 0.0)
	}
	avg := sb.RollingAvg()
	if avg < 0.4 || avg > 0.6 {
		t.Errorf("expected avg ~0.5, got %.2f", avg)
	}
}

func TestSB_MinSamplesPreventsPrematureTrip(t *testing.T) {
	sb := agent.NewSemanticBreaker(testCfg()) // minSamples=3
	// Only 2 bad scores — should not trip yet
	record(sb, 0.10)
	record(sb, 0.10)
	if sb.State() != agent.SBHealthy {
		t.Errorf("should not trip with < minSamples, got %s", sb.State())
	}
}
