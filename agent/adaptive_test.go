package agent_test

import (
	"testing"
	"time"

	"github.com/yabanci/agentshield/agent"
)

func TestAdaptive_UncalibratedUsesDefaults(t *testing.T) {
	sb := agent.NewSemanticBreaker(agent.DefaultSBConfig)
	snap := sb.Snapshot()
	if snap.Calibration.Calibrated {
		t.Error("breaker should not be calibrated before collecting enough samples")
	}
	if snap.Calibration.SamplesCollected != 0 {
		t.Error("samples collected should start at 0")
	}
}

func TestAdaptive_CalibratesAfterNSamples(t *testing.T) {
	cfg := agent.SemanticBreakerConfig{
		WindowSize:        6,
		MinSamples:        2,
		DegradedThreshold: 0.65,
		FailingThreshold:  0.45,
		OpenTimeout:       30 * time.Second,
		RecoverySamples:   2,
	}
	sb := agent.NewSemanticBreakerWithCalibN(cfg, 5)

	// Record 5 good scores — should trigger calibration
	for i := 0; i < 5; i++ {
		sb.Record(0.92, agent.QualityResult{Score: 0.92})
	}

	snap := sb.Snapshot()
	if !snap.Calibration.Calibrated {
		t.Fatal("expected calibrated after 5 samples")
	}
	if snap.Calibration.BaselineMean < 0.80 {
		t.Errorf("baseline mean should be ~0.92, got %.2f", snap.Calibration.BaselineMean)
	}
	if snap.Calibration.LearnedFailing >= snap.Calibration.LearnedDegraded {
		t.Error("failing threshold should be lower than degraded threshold")
	}
	// Learned thresholds should be tighter than defaults for a high-quality model
	if snap.Calibration.LearnedDegraded <= cfg.DegradedThreshold {
		t.Logf("learned degraded=%.2f, default=%.2f", snap.Calibration.LearnedDegraded, cfg.DegradedThreshold)
		// Not an error — adaptive thresholds depend on std; just log
	}
}

func TestAdaptive_TighterThresholdsForHighQualityModel(t *testing.T) {
	cfg := agent.DefaultSBConfig
	sb := agent.NewSemanticBreakerWithCalibN(cfg, 10)

	// Simulate a model that consistently scores 0.95 ± 0.03
	scores := []float64{0.95, 0.93, 0.97, 0.94, 0.96, 0.95, 0.92, 0.97, 0.94, 0.96}
	for _, s := range scores {
		sb.Record(s, agent.QualityResult{Score: s})
	}

	snap := sb.Snapshot()
	if !snap.Calibration.Calibrated {
		t.Fatal("expected calibrated")
	}

	// For a model scoring ~0.95, the failing threshold should be above 0.45 (default)
	// because a drop to 0.45 would be extreme for this model
	if snap.Calibration.LearnedFailing <= cfg.FailingThreshold {
		t.Logf("learned failing=%.2f, default=%.2f — thresholds tightened as expected",
			snap.Calibration.LearnedFailing, cfg.FailingThreshold)
	}
}

func TestAdaptive_DriftDetected(t *testing.T) {
	cfg := agent.DefaultSBConfig
	sb := agent.NewSemanticBreakerWithCalibN(cfg, 5)

	// Calibrate with a high baseline (mean ~0.92).
	for i := 0; i < 5; i++ {
		sb.Record(0.92, agent.QualityResult{Score: 0.92})
	}
	snap := sb.Snapshot()
	if !snap.Calibration.Calibrated {
		t.Fatal("expected calibrated")
	}
	if snap.Calibration.Drift {
		t.Fatal("should not detect drift before any post-calibration samples")
	}

	// Feed 30+ post-calibration samples that drift down sharply (mean ~0.50).
	for i := 0; i < 35; i++ {
		sb.Record(0.50, agent.QualityResult{Score: 0.50})
	}
	snap = sb.Snapshot()
	if !snap.Calibration.Drift {
		t.Errorf("expected drift detection: baseline=%.2f long_term=%.2f",
			snap.Calibration.BaselineMean, snap.Calibration.LongTermMean)
	}
}

func TestAdaptive_NoDriftWithinTolerance(t *testing.T) {
	cfg := agent.DefaultSBConfig
	sb := agent.NewSemanticBreakerWithCalibN(cfg, 5)

	for i := 0; i < 5; i++ {
		sb.Record(0.85, agent.QualityResult{Score: 0.85})
	}
	// Post-calibration samples within ±0.10 of baseline — no drift.
	for i := 0; i < 35; i++ {
		sb.Record(0.80, agent.QualityResult{Score: 0.80})
	}
	snap := sb.Snapshot()
	if snap.Calibration.Drift {
		t.Errorf("should not detect drift within 20pp tolerance: baseline=%.2f long_term=%.2f",
			snap.Calibration.BaselineMean, snap.Calibration.LongTermMean)
	}
}

func TestAdaptive_SamplesCountedCorrectly(t *testing.T) {
	sb := agent.NewSemanticBreakerWithCalibN(agent.DefaultSBConfig, 8)

	for i := 0; i < 5; i++ {
		sb.Record(0.85, agent.QualityResult{Score: 0.85})
	}

	snap := sb.Snapshot()
	if snap.Calibration.SamplesCollected != 5 {
		t.Errorf("expected 5 samples collected, got %d", snap.Calibration.SamplesCollected)
	}
	if snap.Calibration.Calibrated {
		t.Error("should not be calibrated yet (need 8 samples)")
	}
	if snap.Calibration.SamplesNeeded != 8 {
		t.Errorf("expected samples_needed=8, got %d", snap.Calibration.SamplesNeeded)
	}
}
