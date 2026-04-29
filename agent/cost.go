// cost.go — token estimation and cost savings tracking.
//
// Prices are estimates for cloud-hosted Llama-class models (USD per 1M tokens).
// The value is in the RELATIVE comparison: cache saves X, fallback saves Y.
package agent

import (
	"sync/atomic"
)

// Estimated pricing in USD per 1M tokens (Groq-class pricing for Llama models).
const (
	costPrimaryPerMToken  = 0.06 // llama3.2 3B — $0.06/1M tokens
	costFallbackPerMToken = 0.02 // llama3.2:1b 1B — $0.02/1M tokens
)

// CostTracker accumulates token usage and cost estimates per tier.
type CostTracker struct {
	primaryTokens  atomic.Int64
	fallbackTokens atomic.Int64
	cacheTokens    atomic.Int64 // tokens SAVED by serving from cache
	// tier request counts for availability score
	primaryReqs  atomic.Int64
	fallbackReqs atomic.Int64
	cacheReqs    atomic.Int64
	deniedReqs   atomic.Int64
}

func newCostTracker() *CostTracker { return &CostTracker{} }

// NewTestCostTracker creates a CostTracker for use in tests.
func NewTestCostTracker() *CostTracker { return newCostTracker() }

// Record registers a completed request for cost tracking.
func (c *CostTracker) Record(tier Tier, responseText string) {
	tokens := estimateTokens(responseText)
	switch tier {
	case TierPrimary:
		c.primaryTokens.Add(int64(tokens))
		c.primaryReqs.Add(1)
	case TierFallback:
		c.fallbackTokens.Add(int64(tokens))
		c.fallbackReqs.Add(1)
	case TierCache:
		c.cacheTokens.Add(int64(tokens)) // tokens we didn't pay for
		c.cacheReqs.Add(1)
	case TierDegraded:
		c.deniedReqs.Add(1)
	}
}

// CostStats is a snapshot of current spending and savings.
type CostStats struct {
	PrimaryTokens   int64   `json:"primary_tokens"`
	FallbackTokens  int64   `json:"fallback_tokens"`
	CachedTokens    int64   `json:"cached_tokens"`
	SpentPrimary    float64 `json:"spent_primary_usd"`
	SpentFallback   float64 `json:"spent_fallback_usd"`
	SavedByCache    float64 `json:"saved_by_cache_usd"`
	SavedByFallback float64 `json:"saved_by_fallback_usd"`
	TotalSaved      float64 `json:"total_saved_usd"`
	SavingsPercent  float64 `json:"savings_percent"`
}

// Snapshot returns current cost statistics.
func (c *CostTracker) Snapshot() CostStats {
	pt := c.primaryTokens.Load()
	ft := c.fallbackTokens.Load()
	ct := c.cacheTokens.Load()

	spentPrimary := tokensToUSD(pt, costPrimaryPerMToken)
	spentFallback := tokensToUSD(ft, costFallbackPerMToken)

	// Cache savings: these tokens would have cost primary rate
	savedByCache := tokensToUSD(ct, costPrimaryPerMToken)
	// Fallback savings: we paid fallback rate instead of primary rate
	savedByFallback := tokensToUSD(ft, costPrimaryPerMToken-costFallbackPerMToken)

	totalSaved := savedByCache + savedByFallback
	totalWouldHaveSpent := spentPrimary + spentFallback + savedByCache + savedByFallback
	savingsPercent := 0.0
	if totalWouldHaveSpent > 0 {
		savingsPercent = totalSaved / totalWouldHaveSpent * 100
	}

	return CostStats{
		PrimaryTokens:   pt,
		FallbackTokens:  ft,
		CachedTokens:    ct,
		SpentPrimary:    spentPrimary,
		SpentFallback:   spentFallback,
		SavedByCache:    savedByCache,
		SavedByFallback: savedByFallback,
		TotalSaved:      totalSaved,
		SavingsPercent:  savingsPercent,
	}
}

// TierCounts returns request counts per tier (for availability score).
func (c *CostTracker) TierCounts() (primary, fallback, cache, denied int64) {
	return c.primaryReqs.Load(), c.fallbackReqs.Load(),
		c.cacheReqs.Load(), c.deniedReqs.Load()
}

// estimateTokens approximates token count from text length.
// Rule of thumb: ~4 chars per token for English text.
func estimateTokens(text string) int {
	if len(text) == 0 {
		return 0
	}
	t := len(text) / 4
	if t < 1 {
		t = 1
	}
	return t
}

func tokensToUSD(tokens int64, pricePerMillion float64) float64 {
	return float64(tokens) / 1_000_000 * pricePerMillion
}
