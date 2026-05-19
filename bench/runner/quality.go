// Package runner drives both paths (naive and AgentShield) against the fake
// backend, collects timing samples, and scores each response.
//
// This file owns the quality rubric used to determine whether a response is
// "useful". The rubric deliberately reuses quality.QualityEvaluator from the
// main module — the bench does NOT duplicate scoring logic.
package runner

import (
	"context"

	"github.com/yabanci/agentshield/quality"
)

// qualityThreshold is the minimum score for a response to be counted as
// "useful" in the bench report. We use QualityAcceptable (0.45) — the same
// threshold the orchestrator uses to reject primary-tier responses.
// Any response above this would pass the AgentShield quality gate; any
// response below it would be rejected and routed to the next tier.
const qualityThreshold = quality.QualityAcceptable

// sharedEval is a singleton evaluator shared across all bench runs.
// It is created without an embedder (nil) so coherence signal is skipped —
// the fake backend serves fixed-length zero vectors that would make every
// response look equally incoherent. The three text-based signals (repetition,
// refusal markers, length anomaly) are sufficient to discriminate between
// good and garbage fake responses.
//
// Thread-safe: QualityEvaluator.Evaluate() holds an internal mutex for the
// rolling-length window.
var sharedEval = quality.NewEvaluator(nil)

// isUseful returns true if the response scores above qualityThreshold.
// An empty response always returns false.
func isUseful(ctx context.Context, prompt, response string) bool {
	if response == "" {
		return false
	}
	qr := sharedEval.Evaluate(ctx, prompt, response)
	return qr.Score >= qualityThreshold
}

// score returns the raw quality score for a response, 0.0–1.0.
func score(ctx context.Context, prompt, response string) float64 {
	if response == "" {
		return 0
	}
	return sharedEval.Evaluate(ctx, prompt, response).Score
}
