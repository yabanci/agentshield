// latency.go — rolling-window p95 latency tracker per tier.
package telemetry

import (
	"sort"
	"sync"
	"time"
)

const latencyWindowSize = 100 // last 100 requests per tier

// LatencyTracker maintains rolling latency windows per tier.
// Computes p95 on demand via sort (O(n log n) on a 100-element window — fine).
type LatencyTracker struct {
	mu      sync.Mutex
	windows map[Tier][]time.Duration
	idx     map[Tier]int
	filled  map[Tier]bool
}

func NewLatencyTracker() *LatencyTracker {
	return &LatencyTracker{
		windows: make(map[Tier][]time.Duration),
		idx:     make(map[Tier]int),
		filled:  make(map[Tier]bool),
	}
}

// NewTestLatencyTracker creates a LatencyTracker for use in tests.
func NewTestLatencyTracker() *LatencyTracker { return NewLatencyTracker() }

// Record adds a latency sample for the given tier.
func (l *LatencyTracker) Record(tier Tier, d time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	w, ok := l.windows[tier]
	if !ok {
		w = make([]time.Duration, latencyWindowSize)
		l.windows[tier] = w
	}
	w[l.idx[tier]] = d
	l.idx[tier] = (l.idx[tier] + 1) % latencyWindowSize
	if l.idx[tier] == 0 {
		l.filled[tier] = true
	}
}

// P95 returns the 95th-percentile latency in milliseconds for a tier.
// Returns 0 if no samples yet.
func (l *LatencyTracker) P95(tier Tier) int64 {
	return l.percentile(tier, 95)
}

// P50 returns the median latency in milliseconds for a tier.
func (l *LatencyTracker) P50(tier Tier) int64 {
	return l.percentile(tier, 50)
}

// P99 returns the 99th-percentile latency in milliseconds for a tier.
func (l *LatencyTracker) P99(tier Tier) int64 {
	return l.percentile(tier, 99)
}

// percentile computes the requested percentile (0-100) from the rolling
// window. Single helper backing P50/P95/P99 so the sort-then-rank logic
// is in one place and stays consistent across all three.
func (l *LatencyTracker) percentile(tier Tier, p int) int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	w, ok := l.windows[tier]
	if !ok {
		return 0
	}
	n := latencyWindowSize
	if !l.filled[tier] {
		n = l.idx[tier]
	}
	if n == 0 {
		return 0
	}
	cp := make([]time.Duration, n)
	copy(cp, w[:n])
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	rank := (n * p) / 100
	if rank >= n {
		rank = n - 1
	}
	return cp[rank].Milliseconds()
}

// TierLatency is per-tier P50/P95/P99 + sample count for one tier.
type TierLatency struct {
	P50MS   int64 `json:"p50_ms"`
	P95MS   int64 `json:"p95_ms"`
	P99MS   int64 `json:"p99_ms"`
	Samples int   `json:"samples"`
}

// LatencySnapshot reports per-tier latency percentiles. The flat
// PrimaryP95MS / FallbackP95MS / CacheP95MS fields are kept for
// backward compatibility with existing dashboards / Prometheus scrapers;
// the new ByTier map carries the full P50/P95/P99 breakdown the dashboard
// histogram needs.
type LatencySnapshot struct {
	PrimaryP95MS  int64 `json:"primary_p95_ms"`
	FallbackP95MS int64 `json:"fallback_p95_ms"`
	CacheP95MS    int64 `json:"cache_p95_ms"`

	ByTier map[string]TierLatency `json:"by_tier"`
}

// tierBreakdownLocked computes P50/P95/P99/Samples for a tier with the
// caller's lock already held. Keeps all four values from the same window
// snapshot so a concurrent Record() can't make Samples disagree with the
// percentiles.
func (l *LatencyTracker) tierBreakdownLocked(tier Tier) TierLatency {
	w, ok := l.windows[tier]
	if !ok {
		return TierLatency{}
	}
	n := latencyWindowSize
	if !l.filled[tier] {
		n = l.idx[tier]
	}
	if n == 0 {
		return TierLatency{}
	}
	cp := make([]time.Duration, n)
	copy(cp, w[:n])
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	pct := func(p int) int64 {
		rank := (n * p) / 100
		if rank >= n {
			rank = n - 1
		}
		return cp[rank].Milliseconds()
	}
	return TierLatency{
		P50MS:   pct(50),
		P95MS:   pct(95),
		P99MS:   pct(99),
		Samples: n,
	}
}

// Snapshot returns a consistent view of per-tier latency under a single
// lock acquisition so percentile values and sample counts never disagree
// with each other (round-2 audit H-2).
func (l *LatencyTracker) Snapshot() LatencySnapshot {
	l.mu.Lock()
	defer l.mu.Unlock()
	primary := l.tierBreakdownLocked(TierPrimary)
	fallback := l.tierBreakdownLocked(TierFallback)
	cache := l.tierBreakdownLocked(TierCache)
	return LatencySnapshot{
		PrimaryP95MS:  primary.P95MS,
		FallbackP95MS: fallback.P95MS,
		CacheP95MS:    cache.P95MS,
		ByTier: map[string]TierLatency{
			string(TierPrimary):  primary,
			string(TierFallback): fallback,
			string(TierCache):    cache,
		},
	}
}
