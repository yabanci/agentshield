package agent_test

import (
	"testing"

	"github.com/yabanci/agentshield/agent"
)

func TestCostTracker_RecordsPrimaryTokens(t *testing.T) {
	ct := agent.NewTestCostTracker()
	ct.Record(agent.TierPrimary, "Hello world this is a response")

	s := ct.Snapshot()
	if s.PrimaryTokens == 0 {
		t.Error("expected non-zero primary tokens")
	}
	if s.SpentPrimary == 0 {
		t.Error("expected non-zero primary cost")
	}
}

func TestCostTracker_CacheIsFree(t *testing.T) {
	ct := agent.NewTestCostTracker()
	ct.Record(agent.TierCache, "cached response text here")

	s := ct.Snapshot()
	if s.SpentPrimary != 0 || s.SpentFallback != 0 {
		t.Error("cache tier should not incur cost")
	}
	if s.CachedTokens == 0 {
		t.Error("expected cached tokens to be recorded")
	}
	if s.SavedByCache == 0 {
		t.Error("expected non-zero savings from cache")
	}
}

func TestCostTracker_FallbackCheaperThanPrimary(t *testing.T) {
	ct := agent.NewTestCostTracker()
	text := "this is a typical response of reasonable length for testing"

	// Record same text through both tiers
	ct.Record(agent.TierPrimary, text)
	ct.Record(agent.TierFallback, text)

	s := ct.Snapshot()
	if s.SpentFallback >= s.SpentPrimary {
		t.Errorf("fallback should cost less than primary: fallback=%.6f primary=%.6f",
			s.SpentFallback, s.SpentPrimary)
	}
}

func TestCostTracker_SavingsPercentIncreases(t *testing.T) {
	ct := agent.NewTestCostTracker()
	text := "response text for cost testing purposes here"

	ct.Record(agent.TierPrimary, text)
	s1 := ct.Snapshot()

	// Add cache hits — savings should increase
	ct.Record(agent.TierCache, text)
	ct.Record(agent.TierCache, text)
	s2 := ct.Snapshot()

	if s2.SavingsPercent <= s1.SavingsPercent {
		t.Errorf("savings percent should increase with cache hits: before=%.1f%% after=%.1f%%",
			s1.SavingsPercent, s2.SavingsPercent)
	}
}

func TestCostTracker_TierCounts(t *testing.T) {
	ct := agent.NewTestCostTracker()
	ct.Record(agent.TierPrimary, "a")
	ct.Record(agent.TierPrimary, "b")
	ct.Record(agent.TierFallback, "c")
	ct.Record(agent.TierCache, "d")
	ct.Record(agent.TierDegraded, "")

	pr, fr, cr, dr := ct.TierCounts()
	if pr != 2 { t.Errorf("expected 2 primary, got %d", pr) }
	if fr != 1 { t.Errorf("expected 1 fallback, got %d", fr) }
	if cr != 1 { t.Errorf("expected 1 cache, got %d", cr) }
	if dr != 1 { t.Errorf("expected 1 denied, got %d", dr) }
}
