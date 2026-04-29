// semantic_breaker.go
//
// SemanticBreaker is a circuit breaker that opens on quality degradation,
// not transport errors. A model can be returning HTTP 200 while this
// breaker is OPEN — that's the point.
//
// States:
//   Healthy  — quality is fine, requests pass through
//   Degraded — quality is slipping, requests pass but we log warnings
//   Failing  — quality is consistently bad, requests are blocked (circuit OPEN)
package agent

import (
	"fmt"
	"sync"
	"time"
)

// SBState is the state of a SemanticBreaker.
type SBState string

const (
	SBHealthy  SBState = "healthy"
	SBDegraded SBState = "degraded"
	SBFailing  SBState = "failing" // circuit open
)

// SemanticBreakerConfig controls thresholds.
type SemanticBreakerConfig struct {
	WindowSize        int           // how many recent scores to track
	MinSamples        int           // need at least this many before tripping
	DegradedThreshold float64       // rolling avg below this → Degraded
	FailingThreshold  float64       // rolling avg below this → Failing (open)
	OpenTimeout       time.Duration // how long to stay Failing before probing
	RecoverySamples   int           // consecutive good probes to return to Healthy
}

var DefaultSBConfig = SemanticBreakerConfig{
	WindowSize:        8,
	MinSamples:        3,
	DegradedThreshold: 0.65,
	FailingThreshold:  0.45,
	OpenTimeout:       20 * time.Second,
	RecoverySamples:   2,
}

// SemanticBreaker tracks quality scores and blocks routing when quality
// consistently falls below threshold.
type SemanticBreaker struct {
	mu     sync.Mutex
	cfg    SemanticBreakerConfig
	scores []float64 // circular buffer
	idx    int
	filled bool

	state           SBState
	openAt          time.Time
	consecutiveGood int

	// last evaluation detail for observability (lock-protected)
	lastResult QualityResult
	TripReason string
}

// NewSemanticBreaker creates a breaker with the given config.
func NewSemanticBreaker(cfg SemanticBreakerConfig) *SemanticBreaker {
	return &SemanticBreaker{
		cfg:    cfg,
		scores: make([]float64, cfg.WindowSize),
		state:  SBHealthy,
	}
}

// Record registers a new quality score and updates state.
// Returns the new state.
func (sb *SemanticBreaker) Record(score float64, result QualityResult) SBState {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	sb.lastResult = result
	sb.scores[sb.idx] = score
	sb.idx = (sb.idx + 1) % sb.cfg.WindowSize
	if sb.idx == 0 {
		sb.filled = true
	}

	sb.updateState(score)
	return sb.state
}

func (sb *SemanticBreaker) updateState(latestScore float64) {
	samples := sb.cfg.WindowSize
	if !sb.filled {
		samples = sb.idx
	}
	if samples < sb.cfg.MinSamples {
		return // not enough data yet
	}

	avg := sb.rollingAvg(samples)

	switch sb.state {
	case SBHealthy:
		if avg < sb.cfg.FailingThreshold {
			sb.state = SBFailing
			sb.openAt = time.Now()
			sb.consecutiveGood = 0
			sb.TripReason = fmt.Sprintf("rolling avg quality %.0f%% < failing threshold %.0f%%",
				avg*100, sb.cfg.FailingThreshold*100)
		} else if avg < sb.cfg.DegradedThreshold {
			sb.state = SBDegraded
			sb.TripReason = fmt.Sprintf("rolling avg quality %.0f%% < degraded threshold %.0f%%",
				avg*100, sb.cfg.DegradedThreshold*100)
		}

	case SBDegraded:
		if avg < sb.cfg.FailingThreshold {
			sb.state = SBFailing
			sb.openAt = time.Now()
			sb.consecutiveGood = 0
			sb.TripReason = fmt.Sprintf("quality deteriorated further: avg %.0f%%", avg*100)
		} else if avg >= sb.cfg.DegradedThreshold {
			sb.state = SBHealthy
			sb.TripReason = ""
		}

	case SBFailing:
		// probe window handled in ShouldBlock via openAt check
		if latestScore >= sb.cfg.DegradedThreshold {
			sb.consecutiveGood++
			if sb.consecutiveGood >= sb.cfg.RecoverySamples {
				sb.state = SBHealthy
				sb.TripReason = ""
				sb.consecutiveGood = 0
			}
		} else {
			sb.consecutiveGood = 0
		}
	}
}

// ShouldBlock returns true if this breaker is open and the request
// should be routed to the next tier.
// Returns false (allow probe) after OpenTimeout expires.
func (sb *SemanticBreaker) ShouldBlock() bool {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	if sb.state != SBFailing {
		return false
	}
	if time.Since(sb.openAt) >= sb.cfg.OpenTimeout {
		return false // allow one probe through
	}
	return true
}

// State returns the current state without modifying anything.
func (sb *SemanticBreaker) State() SBState {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.state
}

// RollingAvg returns the current rolling average quality score (0–1).
func (sb *SemanticBreaker) RollingAvg() float64 {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	samples := sb.cfg.WindowSize
	if !sb.filled {
		samples = sb.idx
	}
	if samples == 0 {
		return 1.0 // no data = assume healthy
	}
	return sb.rollingAvg(samples)
}

func (sb *SemanticBreaker) rollingAvg(samples int) float64 {
	sum := 0.0
	for i := 0; i < samples; i++ {
		sum += sb.scores[i]
	}
	return sum / float64(samples)
}

// SBSnapshot is an atomic observability snapshot of a SemanticBreaker.
type SBSnapshot struct {
	State      SBState `json:"state"`
	AvgQuality float64 `json:"avg_quality"`
	TripReason string  `json:"trip_reason,omitempty"`
}

// Snapshot returns a consistent, atomic snapshot (single lock acquisition).
func (sb *SemanticBreaker) Snapshot() SBSnapshot {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	samples := sb.cfg.WindowSize
	if !sb.filled {
		samples = sb.idx
	}
	avg := 1.0
	if samples > 0 {
		avg = sb.rollingAvg(samples)
	}

	return SBSnapshot{
		State:      sb.state,
		AvgQuality: avg,
		TripReason: sb.TripReason,
	}
}
