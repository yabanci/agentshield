package agent_test

import (
	"testing"

	"github.com/yabanci/agentshield/agent"
)

func healthyStatus() agent.Status {
	return agent.Status{
		PrimaryBreaker:  "closed",
		FallbackBreaker: "closed",
		PrimarySemanticCB: agent.SBSnapshot{
			State: agent.SBHealthy, AvgQuality: 0.92,
		},
		FallbackSemanticCB: agent.SBSnapshot{
			State: agent.SBHealthy, AvgQuality: 0.88,
		},
		CacheSize: 40, // fully warm cache
		TierCounts: agent.TierRequestCounts{
			Primary: 80, Fallback: 10, Cache: 30, Denied: 0,
		},
	}
}

func TestScore_HealthySystemScoresHigh(t *testing.T) {
	s := healthyStatus()
	score := agent.ComputeScore(s)

	if score.Total < 80 {
		t.Errorf("healthy system should score >= 80, got %d (breakdown: %+v)",
			score.Total, score.Breakdown)
	}
	if score.Grade != "A" && score.Grade != "B" {
		t.Errorf("expected grade A or B, got %s", score.Grade)
	}
}

func TestScore_KilledPrimaryReducesScore(t *testing.T) {
	s := healthyStatus()
	healthy := agent.ComputeScore(s)

	s.PrimaryKilled = true
	s.PrimaryBreaker = "open"
	degraded := agent.ComputeScore(s)

	if degraded.Total >= healthy.Total {
		t.Errorf("killed primary should reduce score: before=%d after=%d",
			healthy.Total, degraded.Total)
	}
	if degraded.Breakdown.TransportHealth >= healthy.Breakdown.TransportHealth {
		t.Error("transport health component should be lower when primary is killed")
	}
}

func TestScore_SemanticFailureReducesQualityComponent(t *testing.T) {
	s := healthyStatus()
	s.PrimarySemanticCB = agent.SBSnapshot{State: agent.SBFailing, AvgQuality: 0.15}

	score := agent.ComputeScore(s)
	if score.Breakdown.SemanticQuality > 10 {
		t.Errorf("failing semantic CB should lower quality component, got %d",
			score.Breakdown.SemanticQuality)
	}
}

func TestScore_BothCBsOpenScoredF(t *testing.T) {
	s := agent.Status{
		PrimaryKilled:  true,
		FallbackKilled: true,
		PrimarySemanticCB: agent.SBSnapshot{State: agent.SBFailing},
		FallbackSemanticCB: agent.SBSnapshot{State: agent.SBFailing},
		TierCounts: agent.TierRequestCounts{Denied: 10},
	}
	score := agent.ComputeScore(s)

	if score.Grade != "D" && score.Grade != "F" {
		t.Errorf("both models killed+failing should score D or F, got %s (score=%d)",
			score.Grade, score.Total)
	}
}

func TestScore_RecommendationSetWhenDegraded(t *testing.T) {
	s := healthyStatus()
	s.PrimaryKilled = true

	score := agent.ComputeScore(s)
	if score.Recommendation == "" {
		t.Error("expected non-empty recommendation when primary is killed")
	}
}

func TestScore_ColdStartGetsFullCachePoints(t *testing.T) {
	s := agent.Status{
		PrimaryBreaker:  "closed",
		FallbackBreaker: "closed",
		PrimarySemanticCB: agent.SBSnapshot{State: agent.SBHealthy},
		FallbackSemanticCB: agent.SBSnapshot{State: agent.SBHealthy},
		// No traffic, no cache entries yet
		CacheSize:  0,
		TierCounts: agent.TierRequestCounts{},
	}
	score := agent.ComputeScore(s)
	if score.Total != 100 {
		t.Errorf("idle healthy system should score 100, got %d (breakdown: %+v)",
			score.Total, score.Breakdown)
	}
	if score.Grade != "A" {
		t.Errorf("expected grade A for idle healthy system, got %s", score.Grade)
	}
}

func TestScore_CacheHitsCountAsAvailable(t *testing.T) {
	s := healthyStatus()
	// All traffic goes to cache — should still be fully available
	s.TierCounts = agent.TierRequestCounts{Cache: 100, Denied: 0}
	score := agent.ComputeScore(s)
	if score.Breakdown.Availability != 25 {
		t.Errorf("100%% cache hits should give full availability, got %d", score.Breakdown.Availability)
	}
}

func TestScore_AllComponentsBounded(t *testing.T) {
	statuses := []agent.Status{
		healthyStatus(),
		{PrimaryKilled: true, FallbackKilled: true},
		{PrimarySemanticCB: agent.SBSnapshot{State: agent.SBFailing},
			FallbackSemanticCB: agent.SBSnapshot{State: agent.SBFailing}},
	}

	for _, s := range statuses {
		score := agent.ComputeScore(s)
		b := score.Breakdown
		if b.TransportHealth < 0 || b.TransportHealth > 25 {
			t.Errorf("transport_health out of range: %d", b.TransportHealth)
		}
		if b.SemanticQuality < 0 || b.SemanticQuality > 25 {
			t.Errorf("semantic_quality out of range: %d", b.SemanticQuality)
		}
		if b.CacheEfficiency < 0 || b.CacheEfficiency > 25 {
			t.Errorf("cache_efficiency out of range: %d", b.CacheEfficiency)
		}
		if b.Availability < 0 || b.Availability > 25 {
			t.Errorf("availability out of range: %d", b.Availability)
		}
		if score.Total < 0 || score.Total > 100 {
			t.Errorf("total score out of range: %d", score.Total)
		}
	}
}
