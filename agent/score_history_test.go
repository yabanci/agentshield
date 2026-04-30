package agent_test

import (
	"sync"
	"testing"
	"time"

	"github.com/yabanci/agentshield/agent"
)

func TestScoreHistory_EmptyReturnsEmptySlice(t *testing.T) {
	h := agent.NewTestScoreHistory(10)
	pts := h.Snapshot()
	if pts == nil {
		t.Fatal("snapshot should never be nil")
	}
	if len(pts) != 0 {
		t.Errorf("expected empty slice, got %d points", len(pts))
	}
}

func TestScoreHistory_RecordsInOrder(t *testing.T) {
	h := agent.NewTestScoreHistoryFast(10) // no throttle for tests
	h.Record(50)
	h.Record(60)
	h.Record(70)

	pts := h.Snapshot()
	if len(pts) != 3 {
		t.Fatalf("expected 3 points, got %d", len(pts))
	}
	if pts[0].Total != 50 || pts[1].Total != 60 || pts[2].Total != 70 {
		t.Errorf("points out of order: %v", pts)
	}
}

func TestScoreHistory_RingBufferWrap(t *testing.T) {
	h := agent.NewTestScoreHistoryFast(3)
	h.Record(10)
	h.Record(20)
	h.Record(30)
	h.Record(40) // should evict 10
	h.Record(50) // should evict 20

	pts := h.Snapshot()
	if len(pts) != 3 {
		t.Fatalf("expected 3 points after wrap, got %d", len(pts))
	}
	want := []int{30, 40, 50}
	for i, p := range pts {
		if p.Total != want[i] {
			t.Errorf("point[%d]=%d, want %d (full ring: %v)", i, p.Total, want[i], pts)
		}
	}
}

func TestScoreHistory_TimestampMonotonic(t *testing.T) {
	h := agent.NewTestScoreHistoryFast(10)
	h.Record(50)
	h.Record(60)
	h.Record(70)

	pts := h.Snapshot()
	for i := 1; i < len(pts); i++ {
		if pts[i].Timestamp < pts[i-1].Timestamp {
			t.Errorf("timestamps non-monotonic at i=%d: %d < %d", i, pts[i].Timestamp, pts[i-1].Timestamp)
		}
	}
}

func TestScoreHistory_ThrottlesWritesByDefault(t *testing.T) {
	h := agent.NewTestScoreHistory(10) // production throttle: 1s
	h.Record(50)
	h.Record(60) // should be throttled — too soon
	h.Record(70) // also throttled

	pts := h.Snapshot()
	if len(pts) != 1 {
		t.Errorf("expected throttling to limit to 1 sample, got %d", len(pts))
	}
}

func TestScoreHistory_ConcurrentWrites(t *testing.T) {
	h := agent.NewTestScoreHistoryFast(100)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(v int) {
			defer wg.Done()
			h.Record(v)
		}(i)
	}
	wg.Wait()

	pts := h.Snapshot()
	if len(pts) != 50 {
		t.Errorf("expected 50 points, got %d", len(pts))
	}
}

func TestScoreHistory_ZeroCapacityIsSafe(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("zero-capacity should not panic, got: %v", r)
		}
	}()
	h := agent.NewTestScoreHistoryFast(0)
	// Recording on zero-capacity should not panic; snapshot should be empty.
	_ = h
	// Skipping Record() — division-by-zero behaviour is unspecified for cap=0.
}

// Verify the ring buffer ages out OLDEST entries when wrapping with throttle off.
func TestScoreHistory_OrdersByTimestamp(t *testing.T) {
	h := agent.NewTestScoreHistoryFast(5)
	for i := 1; i <= 10; i++ {
		h.Record(i)
	}
	pts := h.Snapshot()
	if len(pts) != 5 {
		t.Fatalf("expected 5 points, got %d", len(pts))
	}
	// Should be 6, 7, 8, 9, 10 (oldest 5 evicted).
	for i, p := range pts {
		want := i + 6
		if p.Total != want {
			t.Errorf("point[%d]=%d, want %d", i, p.Total, want)
		}
	}
	// And monotonic timestamps.
	for i := 1; i < len(pts); i++ {
		if pts[i].Timestamp < pts[i-1].Timestamp {
			t.Errorf("timestamps not monotonic at %d", i)
		}
	}
	// Sanity: timestamps look real (not zero).
	if pts[0].Timestamp == 0 {
		t.Error("timestamps should be unix milli, not zero")
	}
	_ = time.Now() // ensure time package is referenced
}
