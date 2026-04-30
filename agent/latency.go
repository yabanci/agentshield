// latency.go — rolling-window p95 latency tracker per tier.
package agent

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

func newLatencyTracker() *LatencyTracker {
	return &LatencyTracker{
		windows: make(map[Tier][]time.Duration),
		idx:     make(map[Tier]int),
		filled:  make(map[Tier]bool),
	}
}

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
	rank := (n * 95) / 100
	if rank >= n {
		rank = n - 1
	}
	return cp[rank].Milliseconds()
}

// Snapshot returns p95 for all tiers seen so far.
type LatencySnapshot struct {
	PrimaryP95MS  int64 `json:"primary_p95_ms"`
	FallbackP95MS int64 `json:"fallback_p95_ms"`
	CacheP95MS    int64 `json:"cache_p95_ms"`
}

func (l *LatencyTracker) Snapshot() LatencySnapshot {
	return LatencySnapshot{
		PrimaryP95MS:  l.P95(TierPrimary),
		FallbackP95MS: l.P95(TierFallback),
		CacheP95MS:    l.P95(TierCache),
	}
}
