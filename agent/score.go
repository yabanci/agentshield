// score.go — composite Resilience Score (0–100).
//
// Four equally-weighted components, 25 points each:
//   Transport Health  — are the transport circuit breakers healthy?
//   Semantic Quality  — are responses semantically good?
//   Cache Efficiency  — how well is the cache absorbing traffic?
//   Availability      — what % of requests get a real (non-denial) response?
package agent

import "fmt"

// ResilienceScore is the overall health of the agent's resilience stack.
type ResilienceScore struct {
	Total          int                  `json:"total"`                    // 0–100
	Grade          string               `json:"grade"`                    // A/B/C/D/F
	Breakdown      ResilienceBreakdown  `json:"breakdown"`
	Recommendation string               `json:"recommendation,omitempty"` // what to fix
}

// ResilienceBreakdown shows each component's contribution.
type ResilienceBreakdown struct {
	TransportHealth int `json:"transport_health"` // 0–25
	SemanticQuality int `json:"semantic_quality"` // 0–25
	CacheEfficiency int `json:"cache_efficiency"` // 0–25
	Availability    int `json:"availability"`     // 0–25
}

// ComputeScore calculates the resilience score from live status.
func ComputeScore(s Status) ResilienceScore {
	b := ResilienceBreakdown{}

	// ── Component 1: Transport Health (25 pts) ─────────────────────────────
	b.TransportHealth = 25
	if s.PrimaryKilled || s.PrimaryBreaker == "open" {
		b.TransportHealth -= 10
	} else if s.PrimaryBreaker == "half-open" {
		b.TransportHealth -= 4
	}
	if s.FallbackKilled || s.FallbackBreaker == "open" {
		b.TransportHealth -= 10
	} else if s.FallbackBreaker == "half-open" {
		b.TransportHealth -= 4
	}
	if b.TransportHealth < 0 {
		b.TransportHealth = 0
	}

	// ── Component 2: Semantic Quality (25 pts) ─────────────────────────────
	// Primary contributes 15pts, fallback 10pts
	pState := s.PrimarySemanticCB.State
	fState := s.FallbackSemanticCB.State

	pScore := 0
	switch pState {
	case SBHealthy:
		pScore = 15
	case SBDegraded:
		pScore = 8
	case SBFailing:
		pScore = 0
	}
	if s.DegradeMode {
		pScore = 0 // degrade mode active = known bad quality
	}

	fScore := 0
	switch fState {
	case SBHealthy:
		fScore = 10
	case SBDegraded:
		fScore = 5
	case SBFailing:
		fScore = 0
	}
	b.SemanticQuality = pScore + fScore

	// ── Component 3: Cache Efficiency (25 pts) ─────────────────────────────
	// Uses CacheSize as a proxy for cache warmth (0 = cold, 40+ = fully warm).
	// Returns full points when there's no traffic yet (cold start is not a failure).
	totalForEff := s.TierCounts.Primary + s.TierCounts.Fallback +
		s.TierCounts.Cache + s.TierCounts.Denied
	if totalForEff == 0 {
		b.CacheEfficiency = 25 // idle system — not penalised
	} else {
		size := s.CacheSize
		if size > 40 {
			size = 40
		}
		b.CacheEfficiency = int(float64(size)*25.0/40.0 + 0.5) // round, not truncate
		// Bonus for measurable cost savings (max +5, capped at 25)
		if s.Costs.SavingsPercent > 0 {
			bonus := int(s.Costs.SavingsPercent / 100 * 5)
			b.CacheEfficiency += bonus
			if b.CacheEfficiency > 25 {
				b.CacheEfficiency = 25
			}
		}
	}

	// ── Component 4: Availability (25 pts) ─────────────────────────────────
	// % of requests that received a real response (primary, fallback, OR cache).
	// Only graceful denials count against availability.
	totalReqs := s.TierCounts.Primary + s.TierCounts.Fallback +
		s.TierCounts.Cache + s.TierCounts.Denied
	if totalReqs == 0 {
		b.Availability = 25 // no traffic yet = assume healthy
	} else {
		served := totalReqs - s.TierCounts.Denied // all tiers except graceful denial
		b.Availability = int(float64(served) / float64(totalReqs) * 25)
	}

	total100 := b.TransportHealth + b.SemanticQuality + b.CacheEfficiency + b.Availability

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
	if b.TransportHealth < 20 {
		if s.PrimaryKilled {
			return "Primary model is killed — restore it to recover transport health"
		}
		return "Transport circuit breaker is open — check model connectivity"
	}
	if b.SemanticQuality < 15 {
		if s.DegradeMode {
			return "Degrade mode is active — disable it to restore semantic quality"
		}
		return fmt.Sprintf("Primary quality %.0f%% — model may be degraded",
			s.PrimarySemanticCB.AvgQuality*100)
	}
	if b.CacheEfficiency < 10 {
		return "Cache is cold — send repeated queries to build hit rate"
	}
	if b.Availability < 20 {
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
