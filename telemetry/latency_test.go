package telemetry_test

import (
	"sync"
	"testing"
	"time"

	"github.com/yabanci/agentshield/telemetry"
)

func TestLatency_EmptyReturnsZero(t *testing.T) {
	lt := telemetry.NewTestLatencyTracker()
	if got := lt.P95(telemetry.TierPrimary); got != 0 {
		t.Errorf("empty tracker should return 0 p95, got %d", got)
	}
}

func TestLatency_SingleSample(t *testing.T) {
	lt := telemetry.NewTestLatencyTracker()
	lt.Record(telemetry.TierPrimary, 100*time.Millisecond)
	if got := lt.P95(telemetry.TierPrimary); got != 100 {
		t.Errorf("single 100ms sample should give p95=100, got %d", got)
	}
}

func TestLatency_P95IsCorrect(t *testing.T) {
	lt := telemetry.NewTestLatencyTracker()
	// Record 100 samples: 1ms, 2ms, ..., 100ms.
	// p95 = 95th rank = 95ms.
	for i := 1; i <= 100; i++ {
		lt.Record(telemetry.TierPrimary, time.Duration(i)*time.Millisecond)
	}
	got := lt.P95(telemetry.TierPrimary)
	// rank = (100 * 95) / 100 = 95 → index 95 → value 96ms (sorted starts at 1ms@idx 0)
	if got != 96 {
		t.Errorf("expected p95=96ms (95th of 1..100), got %d", got)
	}
}

func TestLatency_RingBufferWrap(t *testing.T) {
	lt := telemetry.NewTestLatencyTracker()
	// Fill window twice; only the last 100 samples should be considered.
	for i := 1; i <= 100; i++ {
		lt.Record(telemetry.TierPrimary, time.Duration(i)*time.Millisecond)
	}
	// Now overwrite with constant 500ms.
	for i := 0; i < 100; i++ {
		lt.Record(telemetry.TierPrimary, 500*time.Millisecond)
	}
	if got := lt.P95(telemetry.TierPrimary); got != 500 {
		t.Errorf("after wrap with all 500ms, expected p95=500, got %d", got)
	}
}

func TestLatency_PerTierIsolated(t *testing.T) {
	lt := telemetry.NewTestLatencyTracker()
	lt.Record(telemetry.TierPrimary, 1*time.Second)
	lt.Record(telemetry.TierFallback, 100*time.Millisecond)
	lt.Record(telemetry.TierCache, 1*time.Millisecond)

	if lt.P95(telemetry.TierPrimary) != 1000 {
		t.Errorf("primary p95 wrong: %d", lt.P95(telemetry.TierPrimary))
	}
	if lt.P95(telemetry.TierFallback) != 100 {
		t.Errorf("fallback p95 wrong: %d", lt.P95(telemetry.TierFallback))
	}
	if lt.P95(telemetry.TierCache) != 1 {
		t.Errorf("cache p95 wrong: %d", lt.P95(telemetry.TierCache))
	}
}

func TestLatency_ConcurrentRecords(t *testing.T) {
	lt := telemetry.NewTestLatencyTracker()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(d int) {
			defer wg.Done()
			lt.Record(telemetry.TierPrimary, time.Duration(d)*time.Millisecond)
		}(i + 1)
	}
	wg.Wait()
	if got := lt.P95(telemetry.TierPrimary); got <= 0 {
		t.Errorf("expected non-zero p95 after concurrent records, got %d", got)
	}
}

func TestLatency_Snapshot(t *testing.T) {
	lt := telemetry.NewTestLatencyTracker()
	lt.Record(telemetry.TierPrimary, 100*time.Millisecond)
	lt.Record(telemetry.TierFallback, 200*time.Millisecond)

	snap := lt.Snapshot()
	if snap.PrimaryP95MS != 100 {
		t.Errorf("snapshot.primary p95 wrong: %d", snap.PrimaryP95MS)
	}
	if snap.FallbackP95MS != 200 {
		t.Errorf("snapshot.fallback p95 wrong: %d", snap.FallbackP95MS)
	}
	if snap.CacheP95MS != 0 {
		t.Errorf("cache should be 0 (no samples), got %d", snap.CacheP95MS)
	}
}
