package telemetry_test

import (
	"testing"

	"github.com/yabanci/agentshield/telemetry"
	"github.com/yabanci/agentshield/quality"
)

func healthyStatus() telemetry.ScoreInput {
	return telemetry.ScoreInput{
		PrimaryBreaker:  "closed",
		FallbackBreaker: "closed",
		PrimarySemanticCB: quality.SBSnapshot{
			State: quality.SBHealthy, AvgQuality: 0.92,
		},
		FallbackSemanticCB: quality.SBSnapshot{
			State: quality.SBHealthy, AvgQuality: 0.88,
		},
		CacheSize: 40, // fully warm cache
		TierCounts: telemetry.TierRequestCounts{
			Primary: 80, Fallback: 10, Cache: 30, Denied: 0,
		},
	}
}

func TestScore_HealthySystemScoresHigh(t *testing.T) {
	s := healthyStatus()
	score := telemetry.ComputeScore(s)

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
	healthy := telemetry.ComputeScore(s)

	s.PrimaryKilled = true
	s.PrimaryBreaker = "open"
	degraded := telemetry.ComputeScore(s)

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
	s.PrimarySemanticCB = quality.SBSnapshot{State: quality.SBFailing, AvgQuality: 0.15}

	score := telemetry.ComputeScore(s)
	// Primary failing → primary contribution = 0; only fallback (8) remains.
	if score.Breakdown.SemanticQuality > 8 {
		t.Errorf("failing semantic CB should lower quality component, got %d",
			score.Breakdown.SemanticQuality)
	}
}

func TestScore_BothCBsOpenScoredF(t *testing.T) {
	s := telemetry.ScoreInput{
		PrimaryKilled:  true,
		FallbackKilled: true,
		PrimarySemanticCB: quality.SBSnapshot{State: quality.SBFailing},
		FallbackSemanticCB: quality.SBSnapshot{State: quality.SBFailing},
		TierCounts: telemetry.TierRequestCounts{Denied: 10},
	}
	score := telemetry.ComputeScore(s)

	if score.Grade != "D" && score.Grade != "F" {
		t.Errorf("both models killed+failing should score D or F, got %s (score=%d)",
			score.Grade, score.Total)
	}
}

func TestScore_RecommendationSetWhenDegraded(t *testing.T) {
	s := healthyStatus()
	s.PrimaryKilled = true

	score := telemetry.ComputeScore(s)
	if score.Recommendation == "" {
		t.Error("expected non-empty recommendation when primary is killed")
	}
}

func TestScore_ColdStartGetsFullCachePoints(t *testing.T) {
	s := telemetry.ScoreInput{
		PrimaryBreaker:  "closed",
		FallbackBreaker: "closed",
		PrimarySemanticCB: quality.SBSnapshot{State: quality.SBHealthy},
		FallbackSemanticCB: quality.SBSnapshot{State: quality.SBHealthy},
		// No traffic, no cache entries yet
		CacheSize:  0,
		TierCounts: telemetry.TierRequestCounts{},
	}
	score := telemetry.ComputeScore(s)
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
	s.TierCounts = telemetry.TierRequestCounts{Cache: 100, Denied: 0}
	score := telemetry.ComputeScore(s)
	if score.Breakdown.Availability != 20 {
		t.Errorf("100%% cache hits should give full availability (20), got %d",
			score.Breakdown.Availability)
	}
}

func TestScore_AllComponentsBounded(t *testing.T) {
	statuses := []telemetry.ScoreInput{
		healthyStatus(),
		{PrimaryKilled: true, FallbackKilled: true},
		{PrimarySemanticCB: quality.SBSnapshot{State: quality.SBFailing},
			FallbackSemanticCB: quality.SBSnapshot{State: quality.SBFailing}},
	}

	for _, s := range statuses {
		score := telemetry.ComputeScore(s)
		b := score.Breakdown
		if b.TransportHealth < 0 || b.TransportHealth > 20 {
			t.Errorf("transport_health out of range: %d", b.TransportHealth)
		}
		if b.SemanticQuality < 0 || b.SemanticQuality > 20 {
			t.Errorf("semantic_quality out of range: %d", b.SemanticQuality)
		}
		if b.CacheEfficiency < 0 || b.CacheEfficiency > 20 {
			t.Errorf("cache_efficiency out of range: %d", b.CacheEfficiency)
		}
		if b.Availability < 0 || b.Availability > 20 {
			t.Errorf("availability out of range: %d", b.Availability)
		}
		if b.Latency < 0 || b.Latency > 20 {
			t.Errorf("latency out of range: %d", b.Latency)
		}
		if score.Total < 0 || score.Total > 100 {
			t.Errorf("total score out of range: %d", score.Total)
		}
	}
}

func TestScore_LatencyAffectsScore(t *testing.T) {
	s := healthyStatus()
	s.Latency.PrimaryP95MS = 0
	healthy := telemetry.ComputeScore(s)

	s.Latency.PrimaryP95MS = 8000 // 8s p95 — clearly slow
	slow := telemetry.ComputeScore(s)

	if slow.Breakdown.Latency >= healthy.Breakdown.Latency {
		t.Errorf("slow p95 should reduce latency component: fast=%d slow=%d",
			healthy.Breakdown.Latency, slow.Breakdown.Latency)
	}
}
