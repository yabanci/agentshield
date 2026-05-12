// score_history.go — rolling buffer of recent Resilience Score snapshots.
package memory

import (
	"sync"
	"time"
)

// ScorePoint is one historical sample of the Resilience Score.
type ScorePoint struct {
	Total     int   `json:"total"`
	Timestamp int64 `json:"ts"` // unix milliseconds
}

// ScoreHistory keeps the last N score samples for sparkline rendering.
// Designed to be sampled periodically (e.g., on each Status() call).
type ScoreHistory struct {
	mu       sync.Mutex
	points   []ScorePoint
	idx      int
	filled   bool
	capacity int
	lastAt   time.Time
	minGap   time.Duration
}

func NewScoreHistory(capacity int) *ScoreHistory {
	return &ScoreHistory{
		points:   make([]ScorePoint, capacity),
		capacity: capacity,
		minGap:   time.Second, // throttle: at most one sample per second
	}
}

// NewTestScoreHistory creates a ScoreHistory with the production 1-second throttle.
func NewTestScoreHistory(capacity int) *ScoreHistory {
	return NewScoreHistory(capacity)
}

// NewTestScoreHistoryFast creates a ScoreHistory with no throttle — for tests
// that need to inject many samples back-to-back.
func NewTestScoreHistoryFast(capacity int) *ScoreHistory {
	h := NewScoreHistory(capacity)
	h.minGap = 0
	return h
}

// Record adds a sample, but throttles to at most one per minGap.
func (h *ScoreHistory) Record(score int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now()
	if !h.lastAt.IsZero() && now.Sub(h.lastAt) < h.minGap {
		return
	}
	h.points[h.idx] = ScorePoint{Total: score, Timestamp: now.UnixMilli()}
	h.idx = (h.idx + 1) % h.capacity
	if h.idx == 0 {
		h.filled = true
	}
	h.lastAt = now
}

// Snapshot returns the points in chronological order.
func (h *ScoreHistory) Snapshot() []ScorePoint {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := h.capacity
	if !h.filled {
		n = h.idx
	}
	if n == 0 {
		return []ScorePoint{}
	}
	out := make([]ScorePoint, 0, n)
	if h.filled {
		// Read from h.idx onwards (oldest first), then wrap to 0..h.idx-1.
		for i := 0; i < h.capacity; i++ {
			pos := (h.idx + i) % h.capacity
			out = append(out, h.points[pos])
		}
	} else {
		for i := 0; i < h.idx; i++ {
			out = append(out, h.points[i])
		}
	}
	return out
}
