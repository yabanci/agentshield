// cost.go — token estimation and cost savings tracking.
//
// Prices are estimates for cloud-hosted Llama-class models (USD per 1M tokens).
// The value is in the RELATIVE comparison: cache saves X, fallback saves Y.
package telemetry

import (
	"sync/atomic"
)

// Estimated pricing in USD per 1M tokens (Groq-class pricing for Llama models).
// Real-world LLM costs are dominated by input tokens (60-80%) when conversation
// history is included, so we track input + output separately.
const (
	costPrimaryInputPerMToken   = 0.03 // llama3.2 3B input
	costPrimaryOutputPerMToken  = 0.06 // llama3.2 3B output
	costFallbackInputPerMToken  = 0.01 // llama3.2:1b input
	costFallbackOutputPerMToken = 0.02 // llama3.2:1b output
)

// CostTracker accumulates token usage and cost estimates per tier.
type CostTracker struct {
	// Input tokens (prompt + conversation history)
	primaryInputTokens  atomic.Int64
	fallbackInputTokens atomic.Int64
	cacheInputTokens    atomic.Int64

	// Output tokens (generated response)
	primaryTokens  atomic.Int64
	fallbackTokens atomic.Int64
	cacheTokens    atomic.Int64 // tokens SAVED by serving from cache

	// tier request counts for availability score
	primaryReqs  atomic.Int64
	fallbackReqs atomic.Int64
	cacheReqs    atomic.Int64
	deniedReqs   atomic.Int64
}

func NewCostTracker() *CostTracker { return &CostTracker{} }

// NewTestCostTracker creates a CostTracker for use in tests.
func NewTestCostTracker() *CostTracker { return NewCostTracker() }

// Record registers a completed request for cost tracking.
//
// inputText is the prompt sent to the LLM (including conversation history
// for React mode). responseText is what came back. Empty inputText is fine
// for tiers that don't call the LLM (cache, degraded).
func (c *CostTracker) Record(tier Tier, inputText, responseText string) {
	inTokens := estimateTokens(inputText)
	outTokens := estimateTokens(responseText)
	switch tier {
	case TierPrimary:
		c.primaryInputTokens.Add(int64(inTokens))
		c.primaryTokens.Add(int64(outTokens))
		c.primaryReqs.Add(1)
	case TierFallback:
		c.fallbackInputTokens.Add(int64(inTokens))
		c.fallbackTokens.Add(int64(outTokens))
		c.fallbackReqs.Add(1)
	case TierCache:
		// Cache "saves" both input + output token costs vs primary
		c.cacheInputTokens.Add(int64(inTokens))
		c.cacheTokens.Add(int64(outTokens))
		c.cacheReqs.Add(1)
	case TierDegraded:
		c.deniedReqs.Add(1)
	}
}

// CostStats is a snapshot of current spending and savings.
type CostStats struct {
	PrimaryInputTokens  int64 `json:"primary_input_tokens"`
	PrimaryOutputTokens int64 `json:"primary_output_tokens"`
	FallbackInputTokens int64 `json:"fallback_input_tokens"`
	FallbackOutputTokens int64 `json:"fallback_output_tokens"`
	CachedInputTokens   int64 `json:"cached_input_tokens"`
	CachedOutputTokens  int64 `json:"cached_output_tokens"`

	// Backwards-compat: total output tokens
	PrimaryTokens  int64 `json:"primary_tokens"`
	FallbackTokens int64 `json:"fallback_tokens"`
	CachedTokens   int64 `json:"cached_tokens"`

	SpentPrimary    float64 `json:"spent_primary_usd"`
	SpentFallback   float64 `json:"spent_fallback_usd"`
	SavedByCache    float64 `json:"saved_by_cache_usd"`
	SavedByFallback float64 `json:"saved_by_fallback_usd"`
	TotalSaved      float64 `json:"total_saved_usd"`
	SavingsPercent  float64 `json:"savings_percent"`
}

// Snapshot returns current cost statistics.
func (c *CostTracker) Snapshot() CostStats {
	pIn, pOut := c.primaryInputTokens.Load(), c.primaryTokens.Load()
	fIn, fOut := c.fallbackInputTokens.Load(), c.fallbackTokens.Load()
	cIn, cOut := c.cacheInputTokens.Load(), c.cacheTokens.Load()

	spentPrimary := tokensToUSD(pIn, costPrimaryInputPerMToken) +
		tokensToUSD(pOut, costPrimaryOutputPerMToken)
	spentFallback := tokensToUSD(fIn, costFallbackInputPerMToken) +
		tokensToUSD(fOut, costFallbackOutputPerMToken)

	// Cache savings: these would have cost primary rates (input+output)
	savedByCache := tokensToUSD(cIn, costPrimaryInputPerMToken) +
		tokensToUSD(cOut, costPrimaryOutputPerMToken)
	// Fallback savings: delta vs primary on the same tokens
	savedByFallback := tokensToUSD(fIn, costPrimaryInputPerMToken-costFallbackInputPerMToken) +
		tokensToUSD(fOut, costPrimaryOutputPerMToken-costFallbackOutputPerMToken)

	totalSaved := savedByCache + savedByFallback
	totalWouldHaveSpent := spentPrimary + spentFallback + savedByCache + savedByFallback
	savingsPercent := 0.0
	if totalWouldHaveSpent > 0 {
		savingsPercent = totalSaved / totalWouldHaveSpent * 100
	}

	return CostStats{
		PrimaryInputTokens:   pIn,
		PrimaryOutputTokens:  pOut,
		FallbackInputTokens:  fIn,
		FallbackOutputTokens: fOut,
		CachedInputTokens:    cIn,
		CachedOutputTokens:   cOut,
		PrimaryTokens:        pOut,
		FallbackTokens:       fOut,
		CachedTokens:         cOut,
		SpentPrimary:         spentPrimary,
		SpentFallback:        spentFallback,
		SavedByCache:         savedByCache,
		SavedByFallback:      savedByFallback,
		TotalSaved:           totalSaved,
		SavingsPercent:       savingsPercent,
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
