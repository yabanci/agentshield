package quality_test

import (
	"testing"
	"time"

	"github.com/yabanci/agentshield/quality"
)

func testCfg() quality.SemanticBreakerConfig {
	return quality.SemanticBreakerConfig{
		WindowSize:        6,
		MinSamples:        3,
		DegradedThreshold: 0.65,
		FailingThreshold:  0.45,
		OpenTimeout:       60 * time.Second, // won't expire in test
		RecoverySamples:   2,
	}
}

func record(sb *quality.SemanticBreaker, score float64) {
	sb.Record(score, quality.QualityResult{Score: score})
}

func TestSB_StartsHealthy(t *testing.T) {
	sb := quality.NewSemanticBreaker(testCfg())
	if sb.State() != quality.SBHealthy {
		t.Errorf("expected healthy, got %s", sb.State())
	}
	if sb.ShouldBlock() {
		t.Error("should not block initially")
	}
}

func TestSB_StaysHealthyOnGoodScores(t *testing.T) {
	sb := quality.NewSemanticBreaker(testCfg())
	for i := 0; i < 6; i++ {
		record(sb, 0.90)
	}
	if sb.State() != quality.SBHealthy {
		t.Errorf("expected healthy, got %s", sb.State())
	}
}

func TestSB_TransitionsToDegradedOnMediumScores(t *testing.T) {
	sb := quality.NewSemanticBreaker(testCfg())
	for i := 0; i < 4; i++ {
		record(sb, 0.50) // below degraded threshold (0.65) but above failing (0.45)
	}
	if sb.State() == quality.SBHealthy {
		t.Error("expected degraded or failing, still healthy")
	}
}

func TestSB_OpensOnBadScores(t *testing.T) {
	sb := quality.NewSemanticBreaker(testCfg())
	for i := 0; i < 4; i++ {
		record(sb, 0.20) // well below failing threshold
	}
	if sb.State() != quality.SBFailing {
		t.Errorf("expected failing, got %s", sb.State())
	}
	if !sb.ShouldBlock() {
		t.Error("should block when failing")
	}
}

func TestSB_TripReason_SetWhenFailing(t *testing.T) {
	sb := quality.NewSemanticBreaker(testCfg())
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
	sb := quality.NewSemanticBreaker(cfg)

	// Trip the breaker
	for i := 0; i < 4; i++ {
		record(sb, 0.10)
	}
	if sb.State() != quality.SBFailing {
		t.Fatal("expected failing state")
	}

	// Recovery — enough good scores
	for i := 0; i < cfg.RecoverySamples; i++ {
		record(sb, 0.90)
	}
	if sb.State() != quality.SBHealthy {
		t.Errorf("expected recovery to healthy, got %s", sb.State())
	}
}

// TestSB_FailingRecovery_MediocreProbesAreNotPunished verifies that probes
// scoring between FailingThreshold and DegradedThreshold do not extend the
// open window (T0-1). Pre-fix, such probes reset openAt and consecutiveGood,
// making recovery impossible if the model healed only partially.
func TestSB_FailingRecovery_MediocreProbesAreNotPunished(t *testing.T) {
	cfg := testCfg()
	cfg.OpenTimeout = 0 // expire immediately so probes are allowed
	sb := quality.NewSemanticBreakerWithCalibN(cfg, 0)

	// Trip the breaker.
	for i := 0; i < 4; i++ {
		record(sb, 0.10)
	}
	if sb.State() != quality.SBFailing {
		t.Fatal("expected failing state")
	}

	// Mediocre probe: between FailingThreshold (0.45) and DegradedThreshold (0.65).
	// Pre-fix this would reset openAt + counter; post-fix it allows next probe.
	record(sb, 0.55)
	if sb.State() != quality.SBFailing {
		t.Fatalf("mediocre probe must not transition out of failing yet, got %s", sb.State())
	}

	// Followed by two full-quality probes — should now recover.
	record(sb, 0.90)
	record(sb, 0.90)
	if sb.State() != quality.SBHealthy {
		t.Errorf("expected healthy after good probes, got %s", sb.State())
	}
}

// TestSB_Calibration_IgnoresBadSamples verifies that scores below
// QualityAcceptable are not included in the calibration window (T0-2).
// Pre-fix, enabling degrade mode before 20 prompts polluted calibration
// permanently — the resulting low thresholds let the breaker never trip.
func TestSB_Calibration_IgnoresBadSamples(t *testing.T) {
	cfg := testCfg()
	cfg.OpenTimeout = 60 * time.Second
	sb := quality.NewSemanticBreakerWithCalibN(cfg, 4)

	// Feed 4 garbage scores — calibration must not complete.
	for i := 0; i < 4; i++ {
		record(sb, 0.10)
	}
	snap := sb.Snapshot()
	if snap.Calibration.Calibrated {
		t.Fatal("calibration must not complete with only sub-acceptable samples")
	}

	// Now feed 4 acceptable scores — calibration should finalize on healthy data.
	for i := 0; i < 4; i++ {
		record(sb, 0.85)
	}
	snap = sb.Snapshot()
	if !snap.Calibration.Calibrated {
		t.Fatal("calibration should complete once 4 acceptable samples arrive")
	}
	if snap.Calibration.BaselineMean < 0.80 {
		t.Errorf("baseline mean should reflect healthy samples (~0.85), got %.2f",
			snap.Calibration.BaselineMean)
	}
}

func TestSB_RollingAvg_AccuracyWithWindow(t *testing.T) {
	sb := quality.NewSemanticBreaker(testCfg())
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
	sb := quality.NewSemanticBreaker(testCfg()) // minSamples=3
	// Only 2 bad scores — should not trip yet
	record(sb, 0.10)
	record(sb, 0.10)
	if sb.State() != quality.SBHealthy {
		t.Errorf("should not trip with < minSamples, got %s", sb.State())
	}
}
