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
package quality

import (
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// StateChangeFunc is called when the breaker transitions between states.
// reason and avgQuality are captured under the lock at transition time.
type StateChangeFunc func(prev, next SBState, reason string, avgQuality float64)

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

// CalibrationInfo describes the result of adaptive threshold learning.
type CalibrationInfo struct {
	Calibrated       bool    `json:"calibrated"`
	SamplesCollected int     `json:"samples_collected"`
	SamplesNeeded    int     `json:"samples_needed"`
	BaselineMean     float64 `json:"baseline_mean,omitempty"`
	BaselineStd      float64 `json:"baseline_std,omitempty"`
	LearnedDegraded  float64 `json:"learned_degraded,omitempty"`
	LearnedFailing   float64 `json:"learned_failing,omitempty"`

	// Drift detection: long-term mean tracked separately from baseline.
	// If |LongTermMean - BaselineMean| > 0.20, Drift is true.
	LongTermMean   float64 `json:"long_term_mean,omitempty"`
	LongTermCount  int     `json:"long_term_count,omitempty"`
	Drift          bool    `json:"drift_detected,omitempty"`
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
	probeInFlight   atomic.Bool // prevents thundering herd in probe window

	// Adaptive calibration — learns thresholds from first N observations.
	calibSamples []float64
	calibN       int
	calibration  CalibrationInfo

	// last evaluation detail for observability (lock-protected)
	lastResult QualityResult
	TripReason string

	// onStateChange fires when the breaker transitions between states.
	onStateChange StateChangeFunc
}

// NewSemanticBreaker creates a breaker with the given config.
// calibN controls how many observations to collect before auto-calibrating
// thresholds (0 = disabled, use cfg thresholds as-is).
func NewSemanticBreaker(cfg SemanticBreakerConfig) *SemanticBreaker {
	return &SemanticBreaker{
		cfg:    cfg,
		scores: make([]float64, cfg.WindowSize),
		state:  SBHealthy,
		calibN: 20, // collect 20 healthy samples before calibrating
		calibration: CalibrationInfo{
			SamplesNeeded: 20,
		},
	}
}

// NewSemanticBreakerWithCalibN creates a breaker with a custom calibration sample count.
// Primarily for testing; production code uses NewSemanticBreaker.
func NewSemanticBreakerWithCalibN(cfg SemanticBreakerConfig, calibN int) *SemanticBreaker {
	sb := NewSemanticBreaker(cfg)
	sb.calibN = calibN
	sb.calibration.SamplesNeeded = calibN
	return sb
}

// WithStateChangeCallback attaches a callback fired on every state transition.
func (sb *SemanticBreaker) WithStateChangeCallback(fn StateChangeFunc) *SemanticBreaker {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	sb.onStateChange = fn
	return sb
}

// Record registers a new quality score and updates state.
// Returns the new state.
func (sb *SemanticBreaker) Record(score float64, result QualityResult) SBState {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	sb.lastResult = result

	// Adaptive calibration: collect healthy-looking scores before calibrating.
	// Only acceptable-quality scores count toward the window — if degrade mode
	// is enabled before baseline is established, the calibration window simply
	// doesn't fill and the breaker keeps its default thresholds. Without this
	// filter, a fresh process whose first 20 calls happen during degrade mode
	// would calibrate to garbage thresholds and never trip again for the
	// process lifetime.
	if !sb.calibration.Calibrated && sb.calibN > 0 {
		if score >= QualityAcceptable {
			sb.calibSamples = append(sb.calibSamples, score)
			sb.calibration.SamplesCollected = len(sb.calibSamples)
			if len(sb.calibSamples) >= sb.calibN {
				sb.calibrate()
			}
		}
	} else if sb.calibration.Calibrated {
		// Drift detection: incrementally update long-term mean.
		// Once we have 30+ post-calibration samples, compare to baseline.
		sb.calibration.LongTermCount++
		n := float64(sb.calibration.LongTermCount)
		sb.calibration.LongTermMean += (score - sb.calibration.LongTermMean) / n
		if sb.calibration.LongTermCount >= 30 {
			diff := sb.calibration.LongTermMean - sb.calibration.BaselineMean
			if diff < 0 {
				diff = -diff
			}
			sb.calibration.Drift = diff > 0.20
		}
	}

	sb.scores[sb.idx] = score
	sb.idx = (sb.idx + 1) % sb.cfg.WindowSize
	if sb.idx == 0 {
		sb.filled = true
	}

	// Release the probe semaphore so the next caller can probe after us.
	if sb.state == SBFailing {
		sb.clearProbe()
	}

	prev := sb.state
	sb.updateState(score)
	if sb.state != prev && sb.onStateChange != nil {
		fn := sb.onStateChange
		p, n := prev, sb.state
		// Capture all values under the lock before launching goroutine.
		reason := sb.TripReason
		samples := sb.cfg.WindowSize
		if !sb.filled {
			samples = sb.idx
		}
		avg := 1.0
		if samples > 0 {
			avg = sb.rollingAvg(samples)
		}
		go fn(p, n, reason, avg)
	}
	return sb.state
}

// calibrate computes adaptive thresholds from the calibration sample window.
// Must be called with lock held.
func (sb *SemanticBreaker) calibrate() {
	n := float64(len(sb.calibSamples))
	mean := 0.0
	for _, s := range sb.calibSamples {
		mean += s
	}
	mean /= n

	variance := 0.0
	for _, s := range sb.calibSamples {
		d := s - mean
		variance += d * d
	}
	variance /= n
	std := math.Sqrt(variance)

	// Thresholds: mean ± 1σ and mean ± 2σ, floored to sane minimums.
	// Enforce failing < degraded with a minimum gap of 0.10 to prevent
	// both collapsing to the same value when std=0 (uniform samples).
	degraded := math.Max(0.40, mean-1.0*std)
	failing := math.Max(0.20, mean-2.0*std)
	if degraded-failing < 0.10 {
		failing = math.Max(0.10, degraded-0.15)
	}

	sb.cfg.DegradedThreshold = degraded
	sb.cfg.FailingThreshold = failing
	sb.calibration = CalibrationInfo{
		Calibrated:       true,
		SamplesCollected: len(sb.calibSamples),
		SamplesNeeded:    sb.calibN,
		BaselineMean:     mean,
		BaselineStd:      std,
		LearnedDegraded:  degraded,
		LearnedFailing:   failing,
	}
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
		switch {
		case latestScore >= sb.cfg.DegradedThreshold:
			// Full-quality probe — count toward recovery.
			sb.consecutiveGood++
			if sb.consecutiveGood >= sb.cfg.RecoverySamples {
				sb.state = SBHealthy
				sb.TripReason = ""
				sb.consecutiveGood = 0
				sb.probeInFlight.Store(false) // reset for next trip
			}
		case latestScore >= sb.cfg.FailingThreshold:
			// Mediocre probe (FailingThreshold ≤ score < DegradedThreshold):
			// quality is improving but not back to baseline. Don't extend
			// openAt — that would keep the breaker stuck — but don't bump
			// consecutiveGood either. The next probe is allowed sooner.
			sb.probeInFlight.Store(false)
		default:
			// Bad probe (<FailingThreshold): reset openAt so the next probe
			// waits the full OpenTimeout. Without this, ShouldBlock() would
			// allow immediate re-probing after a fast-returning bad result.
			sb.consecutiveGood = 0
			sb.openAt = time.Now()
		}
	}
}

// ShouldBlock returns true if this breaker is open and the request
// should be routed to the next tier.
// After OpenTimeout, allows ONE probe through at a time — subsequent
// concurrent callers are still blocked until the probe completes and
// Record() updates the state. This prevents thundering herd on recovery.
func (sb *SemanticBreaker) ShouldBlock() bool {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	if sb.state != SBFailing {
		return false
	}
	if time.Since(sb.openAt) >= sb.cfg.OpenTimeout {
		// Only one probe at a time via CAS; others stay blocked.
		return !sb.probeInFlight.CompareAndSwap(false, true)
	}
	return true
}

// clearProbe releases the probe semaphore. Called automatically by Record()
// when the breaker is in SBFailing state.
func (sb *SemanticBreaker) clearProbe() {
	sb.probeInFlight.Store(false)
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
	State       SBState         `json:"state"`
	AvgQuality  float64         `json:"avg_quality"`
	TripReason  string          `json:"trip_reason,omitempty"`
	Calibration CalibrationInfo `json:"calibration"`
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
		State:       sb.state,
		AvgQuality:  avg,
		TripReason:  sb.TripReason,
		Calibration: sb.calibration,
	}
}
