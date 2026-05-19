package runner_test

import (
	"testing"

	"github.com/yabanci/agentshield/bench/fakebackend"
	"github.com/yabanci/agentshield/bench/runner"
)

func TestRunScenario_Garbage(t *testing.T) {
	srv := fakebackend.New(42)
	defer srv.Close()

	result := runner.RunScenario(runner.ScenarioGarbage, srv.URL(), 10, benchPrompts())
	if result.Scenario != runner.ScenarioGarbage {
		t.Fatalf("wrong scenario: %q", result.Scenario)
	}

	// Naive must return 0% useful (all garbage responses contain refusal markers).
	if result.Naive.Stats.UsefulRate > 0 {
		t.Errorf("naive garbage: expected 0%% useful, got %.0f%%",
			result.Naive.Stats.UsefulRate*100)
	}

	// AgentShield must return non-zero useful (fallback + cache rescue garbage).
	if result.Shield.Stats.UsefulRate <= 0 {
		t.Errorf("shield garbage: expected >0%% useful, got %.0f%%",
			result.Shield.Stats.UsefulRate*100)
	}

	// AgentShield useful must be strictly better than naive.
	if result.Shield.Stats.UsefulRate <= result.Naive.Stats.UsefulRate {
		t.Errorf("shield (%.0f%%) should beat naive (%.0f%%) on garbage",
			result.Shield.Stats.UsefulRate*100, result.Naive.Stats.UsefulRate*100)
	}
}

func TestRunScenario_Down(t *testing.T) {
	srv := fakebackend.New(42)
	defer srv.Close()

	result := runner.RunScenario(runner.ScenarioDown, srv.URL(), 10, benchPrompts())

	// Naive: the backend returns 503 on the first attempt and the one retry.
	// After 2 attempts it errors — success rate should be 0%.
	if result.Naive.Stats.SuccessRate > 0 {
		t.Errorf("naive down: expected 0%% success, got %.0f%%",
			result.Naive.Stats.SuccessRate*100)
	}
	if result.Naive.Stats.UsefulRate > 0 {
		t.Errorf("naive down: expected 0%% useful, got %.0f%%",
			result.Naive.Stats.UsefulRate*100)
	}

	// AgentShield: the graceful denial tier always returns text, so success
	// rate should be 100%. The graceful-denial text scores below
	// qualityThreshold, so useful rate may still be low — but it is defined
	// as a valid response, not an error.
	if result.Shield.Stats.SuccessRate < 1.0 {
		t.Errorf("shield down: expected 100%% success (graceful denial), got %.0f%%",
			result.Shield.Stats.SuccessRate*100)
	}
}

func TestRunScenario_Brownout_ShieldBetterThanNaive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping brownout test in short mode — involves 8s sleeps")
	}
	srv := fakebackend.New(42)
	defer srv.Close()

	// Use only 6 requests to keep the test under 60s (p50 fast cohort ~300ms,
	// worst case slow cohort 9s × 6 = 54s). This is enough to see the gap.
	result := runner.RunScenario(runner.ScenarioBrownout, srv.URL(), 6, benchPrompts())

	// Shield useful rate must be >= naive useful rate.
	if result.Shield.Stats.UsefulRate < result.Naive.Stats.UsefulRate {
		t.Errorf("shield (%.0f%%) must be >= naive (%.0f%%) under brownout",
			result.Shield.Stats.UsefulRate*100, result.Naive.Stats.UsefulRate*100)
	}
}

func TestStats_Percentiles_Monotonic(t *testing.T) {
	srv := fakebackend.New(42)
	defer srv.Close()

	result := runner.RunScenario(runner.ScenarioGarbage, srv.URL(), 20, benchPrompts())

	for _, pr := range []runner.PathResult{result.Naive, result.Shield} {
		s := pr.Stats
		if s.P50MS > s.P95MS {
			t.Errorf("%s: p50 (%d) > p95 (%d)", pr.Path, s.P50MS, s.P95MS)
		}
		if s.P95MS > s.P99MS {
			t.Errorf("%s: p95 (%d) > p99 (%d)", pr.Path, s.P95MS, s.P99MS)
		}
	}
}

func TestStats_UsefulRate_BoundedZeroToOne(t *testing.T) {
	srv := fakebackend.New(42)
	defer srv.Close()

	result := runner.RunScenario(runner.ScenarioGarbage, srv.URL(), 10, benchPrompts())

	for _, pr := range []runner.PathResult{result.Naive, result.Shield} {
		if pr.Stats.UsefulRate < 0 || pr.Stats.UsefulRate > 1 {
			t.Errorf("%s: UsefulRate out of range [0,1]: %f", pr.Path, pr.Stats.UsefulRate)
		}
		if pr.Stats.SuccessRate < 0 || pr.Stats.SuccessRate > 1 {
			t.Errorf("%s: SuccessRate out of range [0,1]: %f", pr.Path, pr.Stats.SuccessRate)
		}
	}
}

func TestQualityRubric_GarbageScoresBelowThreshold(t *testing.T) {
	srv := fakebackend.New(42)
	defer srv.Close()

	result := runner.RunScenario(runner.ScenarioGarbage, srv.URL(), 5, benchPrompts())

	// Every naive sample should have a low quality score.
	for i, s := range result.Naive.Samples {
		if s.Success && s.Score >= 0.45 {
			t.Errorf("naive sample %d: garbage response scored %.2f (expected < 0.45)", i, s.Score)
		}
	}
}

func benchPrompts() []string {
	return []string{
		"Explain how a circuit breaker works in distributed systems.",
		"What is the difference between concurrency and parallelism?",
		"Describe the CAP theorem and its implications.",
	}
}
