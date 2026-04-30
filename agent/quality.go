// Package agent — quality.go
//
// QualityEvaluator scores an LLM response without any external API calls.
// Four independent signals combine into a single 0.0–1.0 quality score.
// Low score → SemanticBreaker records a failure → may open the circuit.
package agent

import (
	"context"
	"math"
	"strings"
	"sync"
	"unicode"
)

// QualitySignal is one triggered quality issue.
type QualitySignal struct {
	Name    string  `json:"name"`
	Penalty float64 `json:"penalty"` // amount subtracted from score
	Detail  string  `json:"detail,omitempty"`
}

// QualityResult is the output of a single evaluation.
type QualityResult struct {
	Score   float64          `json:"score"`   // 0.0 (trash) – 1.0 (perfect)
	Signals []QualitySignal  `json:"signals"` // triggered issues
}

// Thresholds for semantic routing decisions.
const (
	QualityGood      = 0.70 // above this → healthy
	QualityAcceptable = 0.45 // between → degraded
	// below QualityAcceptable → counts as semantic failure in the breaker
)

// QualityEvaluator scores LLM responses.
// It is safe for concurrent use.
type QualityEvaluator struct {
	mu          sync.Mutex
	lengths     []int  // rolling window of past response lengths
	lenIdx      int
	lenFilled   bool
	lenWindow   int
	embedder    Embedder // optional; nil = skip coherence signal
}

func newQualityEvaluator(embedder Embedder) *QualityEvaluator {
	return &QualityEvaluator{
		embedder:  embedder,
		lenWindow: 20,
		lengths:   make([]int, 20),
	}
}

// NewTestQualityEvaluator creates an evaluator for use in tests.
func NewTestQualityEvaluator(embedder Embedder) *QualityEvaluator {
	return newQualityEvaluator(embedder)
}

// Evaluate scores a (prompt, response) pair.
// ctx is used only if the embedder is set; a failed embed skips coherence gracefully.
func (e *QualityEvaluator) Evaluate(ctx context.Context, prompt, response string) QualityResult {
	signals := make([]QualitySignal, 0) // never nil — serializes as [] not null
	score := 1.0

	// ── Signal 1: Repetition (weight 0.45) ──────────────────────────────────
	repScore, repDetail := repetitionScore(response)
	if repScore < 1.0 {
		penalty := (1.0 - repScore) * 0.45
		signals = append(signals, QualitySignal{"repetition", penalty, repDetail})
		score -= penalty
	}

	// ── Signal 2: Length anomaly (weight 0.25) ───────────────────────────────
	// Absolute minimum: responses under 10 chars are always anomalous
	// regardless of baseline (handles "Yes.", "No.", "OK." style responses).
	lenScore, lenDetail := e.lengthScore(len(response))
	if len(response) < 10 && len(response) > 0 {
		lenScore = 0.0
		lenDetail = "response below absolute minimum length"
	}
	if lenScore < 1.0 {
		penalty := (1.0 - lenScore) * 0.25
		signals = append(signals, QualitySignal{"length_anomaly", penalty, lenDetail})
		score -= penalty
	}
	// Only record non-empty lengths in the baseline.
	// Storing 0 would corrupt the rolling average and create false positives.
	if len(response) > 0 {
		e.recordLength(len(response))
	}

	// ── Signal 3: Hallucination markers (weight 0.40) ───────────────────────
	hallScore, hallDetail := hallucinationScore(response)
	if hallScore < 1.0 {
		penalty := (1.0 - hallScore) * 0.40
		signals = append(signals, QualitySignal{"hallucination_marker", penalty, hallDetail})
		score -= penalty
	}

	// ── Signal 4: Coherence (weight 0.20, only when embedder available) ──────
	if e.embedder != nil && prompt != "" {
		cohScore, cohDetail := e.coherenceScore(ctx, prompt, response)
		if cohScore < 1.0 {
			penalty := (1.0 - cohScore) * 0.20
			signals = append(signals, QualitySignal{"low_coherence", penalty, cohDetail})
			score -= penalty
		}
	}

	if score < 0 {
		score = 0
	}
	return QualityResult{Score: score, Signals: signals}
}

// ── Signal implementations ───────────────────────────────────────────────────

// repetitionScore detects looping/repetitive text using trigram deduplication.
func repetitionScore(text string) (float64, string) {
	words := tokenize(text)
	if len(words) < 6 {
		return 1.0, "" // too short to measure
	}

	trigrams := make(map[string]int)
	total := 0
	for i := 0; i <= len(words)-3; i++ {
		key := words[i] + " " + words[i+1] + " " + words[i+2]
		trigrams[key]++
		total++
	}

	duplicates := 0
	for _, count := range trigrams {
		if count > 1 {
			duplicates += count - 1
		}
	}

	ratio := float64(duplicates) / float64(total)
	if ratio < 0.15 {
		return 1.0, ""
	}
	if ratio > 0.60 {
		return 0.0, "response is highly repetitive"
	}
	// linear decay between 0.15 and 0.60
	score := 1.0 - (ratio-0.15)/(0.60-0.15)
	return score, "repeated phrases detected"
}

// lengthScore compares response length to the rolling baseline.
func (e *QualityEvaluator) lengthScore(length int) (float64, string) {
	e.mu.Lock()
	avg := e.avgLength()
	e.mu.Unlock()

	if avg == 0 || length == 0 {
		return 1.0, "" // no baseline yet, or empty response handled separately
	}

	ratio := float64(length) / avg
	if ratio >= 0.35 {
		return 1.0, ""
	}
	if ratio < 0.10 {
		return 0.0, "response extremely short vs baseline"
	}
	score := (ratio - 0.10) / (0.35 - 0.10)
	return score, "response shorter than usual"
}

func (e *QualityEvaluator) recordLength(n int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.lengths[e.lenIdx] = n
	e.lenIdx = (e.lenIdx + 1) % e.lenWindow
	if e.lenIdx == 0 {
		e.lenFilled = true
	}
}

func (e *QualityEvaluator) avgLength() float64 {
	end := e.lenWindow
	if !e.lenFilled {
		end = e.lenIdx
	}
	if end == 0 {
		return 0
	}
	sum := 0
	for i := 0; i < end; i++ {
		sum += e.lengths[i]
	}
	return float64(sum) / float64(end)
}

// hallucinationScore penalises known refusal/hallucination phrases.
var hallucinationPatterns = []string{
	"as an ai language model",
	"i cannot and will not",
	"i am unable to assist",
	"i'm unable to assist",
	"i don't have access to real-time",
	"i apologize, but i cannot",
	"i apologize, but as an",
	"i am just an ai",
	"i'm just an ai",
}

func hallucinationScore(text string) (float64, string) {
	lower := strings.ToLower(text)
	hits := 0
	var matched []string
	for _, p := range hallucinationPatterns {
		if strings.Contains(lower, p) {
			hits++
			matched = append(matched, p)
		}
	}
	if hits == 0 {
		return 1.0, ""
	}
	score := math.Max(0, 1.0-float64(hits)*0.35)
	return score, "matched: " + strings.Join(matched, "; ")
}

// coherenceScore measures semantic relevance of response to prompt.
func (e *QualityEvaluator) coherenceScore(ctx context.Context, prompt, response string) (float64, string) {
	pVec, err := e.embedder(ctx, prompt)
	if err != nil {
		return 1.0, "" // embedder unavailable, skip signal
	}
	rVec, err := e.embedder(ctx, response)
	if err != nil {
		return 1.0, ""
	}

	sim := cosineSimilarity(pVec, rVec)
	if sim >= 0.35 {
		return 1.0, ""
	}
	if sim < 0.10 {
		return 0.0, "response semantically unrelated to prompt"
	}
	score := (sim - 0.10) / (0.35 - 0.10)
	return score, "low semantic relevance to prompt"
}

// tokenize splits text into lowercase words, stripping punctuation.
func tokenize(text string) []string {
	var words []string
	var cur strings.Builder
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			cur.WriteRune(r)
		} else if cur.Len() > 0 {
			words = append(words, cur.String())
			cur.Reset()
		}
	}
	if cur.Len() > 0 {
		words = append(words, cur.String())
	}
	return words
}
