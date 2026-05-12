// score.go — composite Resilience Score (0–100).
//
// Five equally-weighted components, 20 points each:
//   Transport Health  — are the transport circuit breakers healthy?
//   Semantic Quality  — are responses semantically good?
//   Cache Efficiency  — how well is the cache absorbing traffic?
//   Availability      — what % of requests get a real (non-denial) response?
//   Latency           — are responses fast enough to be useful?
package agent

import (
	"fmt"

	"github.com/yabanci/agentshield/quality"
)

// ResilienceScore is the overall health of the agent's resilience stack.
type ResilienceScore struct {
	Total          int                 `json:"total"` // 0–100
	Grade          string              `json:"grade"` // A/B/C/D/F
	Breakdown      ResilienceBreakdown `json:"breakdown"`
	Recommendation string              `json:"recommendation,omitempty"`
}

// ResilienceBreakdown shows each component's contribution (each 0–20).
type ResilienceBreakdown struct {
	TransportHealth int `json:"transport_health"`
	SemanticQuality int `json:"semantic_quality"`
	CacheEfficiency int `json:"cache_efficiency"`
	Availability    int `json:"availability"`
	Latency         int `json:"latency"`
}

// ComputeScore calculates the resilience score from live status.
// Each component is scored out of 20; total is 0–100.
func ComputeScore(s Status) ResilienceScore {
	b := ResilienceBreakdown{}

	// ── Component 1: Transport Health (20 pts) ─────────────────────────────
	b.TransportHealth = 20
	if s.PrimaryKilled || s.PrimaryBreaker == "open" {
		b.TransportHealth -= 8
	} else if s.PrimaryBreaker == "half-open" {
		b.TransportHealth -= 3
	}
	if s.FallbackKilled || s.FallbackBreaker == "open" {
		b.TransportHealth -= 8
	} else if s.FallbackBreaker == "half-open" {
		b.TransportHealth -= 3
	}
	if b.TransportHealth < 0 {
		b.TransportHealth = 0
	}

	// ── Component 2: Semantic Quality (20 pts) ─────────────────────────────
	// Primary contributes 12pts, fallback 8pts.
	pState := s.PrimarySemanticCB.State
	fState := s.FallbackSemanticCB.State

	pScore := 0
	switch pState {
	case quality.SBHealthy:
		pScore = 12
	case quality.SBDegraded:
		pScore = 6
	case quality.SBFailing:
		pScore = 0
	}
	if s.DegradeMode {
		pScore = 0
	}

	fScore := 0
	switch fState {
	case quality.SBHealthy:
		fScore = 8
	case quality.SBDegraded:
		fScore = 4
	case quality.SBFailing:
		fScore = 0
	}
	b.SemanticQuality = pScore + fScore

	// ── Component 3: Cache Efficiency (20 pts) ─────────────────────────────
	totalForEff := s.TierCounts.Primary + s.TierCounts.Fallback +
		s.TierCounts.Cache + s.TierCounts.Denied
	if totalForEff == 0 {
		b.CacheEfficiency = 20
	} else {
		size := s.CacheSize
		if size > 40 {
			size = 40
		}
		b.CacheEfficiency = int(float64(size)*20.0/40.0 + 0.5)
		if s.Costs.SavingsPercent > 0 {
			bonus := int(s.Costs.SavingsPercent / 100 * 4)
			b.CacheEfficiency += bonus
			if b.CacheEfficiency > 20 {
				b.CacheEfficiency = 20
			}
		}
	}

	// ── Component 4: Availability (20 pts) ─────────────────────────────────
	totalReqs := s.TierCounts.Primary + s.TierCounts.Fallback +
		s.TierCounts.Cache + s.TierCounts.Denied
	if totalReqs == 0 {
		b.Availability = 20
	} else {
		served := totalReqs - s.TierCounts.Denied
		b.Availability = int(float64(served) / float64(totalReqs) * 20)
	}

	// ── Component 5: Latency (20 pts) ──────────────────────────────────────
	// Based on primary p95: faster is better. Bands chosen for LLM workloads.
	b.Latency = 20
	p95 := s.Latency.PrimaryP95MS
	switch {
	case p95 == 0:
		b.Latency = 20 // no traffic yet
	case p95 < 1000:
		b.Latency = 20
	case p95 < 3000:
		b.Latency = 16
	case p95 < 5000:
		b.Latency = 12
	case p95 < 10000:
		b.Latency = 6
	default:
		b.Latency = 2
	}

	total100 := b.TransportHealth + b.SemanticQuality + b.CacheEfficiency +
		b.Availability + b.Latency

	return ResilienceScore{
		Total:          total100,
		Grade:          scoreGrade(total100),
		Breakdown:      b,
		Recommendation: scoreRecommendation(b, s),
	}
}

func scoreGrade(score int) string {
	switch {
	case score >= 90:
		return "A"
	case score >= 75:
		return "B"
	case score >= 60:
		return "C"
	case score >= 40:
		return "D"
	default:
		return "F"
	}
}

func scoreRecommendation(b ResilienceBreakdown, s Status) string {
	// Return the single most impactful recommendation.
	if b.TransportHealth < 16 {
		if s.PrimaryKilled {
			return "Primary model is killed — restore it to recover transport health"
		}
		return "Transport circuit breaker is open — check model connectivity"
	}
	if b.SemanticQuality < 12 {
		if s.DegradeMode {
			return "Degrade mode is active — disable it to restore semantic quality"
		}
		return fmt.Sprintf("Primary quality %.0f%% — model may be degraded",
			s.PrimarySemanticCB.AvgQuality*100)
	}
	if b.Latency < 12 {
		return fmt.Sprintf("Primary p95 latency %dms — model is slow", s.Latency.PrimaryP95MS)
	}
	if b.CacheEfficiency < 8 {
		return "Cache is cold — send repeated queries to build hit rate"
	}
	if b.Availability < 16 {
		return "High graceful-denial rate — check model availability"
	}
	return ""
}

// TierRequestCounts tracks per-tier request counts for the score.
type TierRequestCounts struct {
	Primary  int64 `json:"primary"`
	Fallback int64 `json:"fallback"`
	Cache    int64 `json:"cache"`
	Denied   int64 `json:"denied"`
}
