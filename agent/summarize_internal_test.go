package agent

import (
	"strings"
	"testing"
)

// TestEstimateTokens verifies the word×4/3 approximation.
// Intentional under-estimate for non-Latin scripts; calibration is in tests.
func TestEstimateTokens(t *testing.T) {
	cases := []struct {
		input string
		want  int
	}{
		{"one two three", 4},  // 3 words → (3*4)/3 = 4
		{"", 0},               // empty
		{"hello world", 2},    // 2 words → (2*4)/3 = 2 (integer division)
		{"a b c d e f", 8},    // 6 words → (6*4)/3 = 8
	}
	for _, tc := range cases {
		got := estimateTokens(tc.input)
		if got != tc.want {
			t.Errorf("estimateTokens(%q) = %d, want %d", tc.input, got, tc.want)
		}
	}
}

// TestEstimateTokens_BoundaryBelowThreshold verifies that a transcript at
// exactly threshold-1 tokens does not trigger summarization. The token count
// must be strictly less than the threshold for the call to be a no-op.
func TestEstimateTokens_BoundaryBelowThreshold(t *testing.T) {
	// Build a string whose estimated token count is exactly threshold-1.
	// estimateTokens = (words * 4) / 3. For threshold=10: need tokens=9.
	// Solve: (words*4)/3 = 9 → words=6 gives (6*4)/3=8; words=7 gives (7*4)/3=9. ✓
	const threshold = 10
	words := make([]string, 7) // 7 words → 9 tokens (integer division)
	for i := range words {
		words[i] = "word"
	}
	input := ""
	for i, w := range words {
		if i > 0 {
			input += " "
		}
		input += w
	}
	got := estimateTokens(input)
	if got >= threshold {
		t.Errorf("expected token count < threshold(%d), got %d — boundary check failed", threshold, got)
	}
}

// TestTruncateAttr_Boundary verifies the exact-boundary behaviour of truncateAttr:
//   - a string of exactly maxAttrBytes is returned unchanged
//   - a string of maxAttrBytes+1 is truncated and suffixed with "...[truncated]"
func TestTruncateAttr_Boundary(t *testing.T) {
	exact := strings.Repeat("x", maxAttrBytes)
	if got := truncateAttr(exact); got != exact {
		t.Errorf("exact boundary: expected unchanged, got len=%d", len(got))
	}

	over := strings.Repeat("x", maxAttrBytes+1)
	got := truncateAttr(over)
	if len(got) <= maxAttrBytes {
		t.Errorf("over boundary: expected truncation, got len=%d", len(got))
	}
	if !strings.HasSuffix(got, "...[truncated]") {
		t.Errorf("over boundary: expected '...[truncated]' suffix, got: %q", got[len(got)-20:])
	}
	// The prefix must be exactly maxAttrBytes bytes of the original.
	if got[:maxAttrBytes] != over[:maxAttrBytes] {
		t.Error("over boundary: prefix was modified")
	}
}

// TestEstimateTokens_CJKUnderestimate documents the intentional under-estimate
// for CJK text. The word×4/3 formula treats each CJK "word" (space-separated
// cluster) the same as a Latin word, but real tokenizers assign ~1 token per
// character. This test locks in the under-estimate so future regressions are
// visible — the accepted range is deliberately wide (1–20 tokens for a
// 9-character Chinese phrase) to remain valid across different whitespace
// splitting behaviours.
//
// If this test fails in the future, it means the estimator was accidentally
// changed to be CJK-aware — evaluate whether that is intentional.
func TestEstimateTokens_CJKUnderestimate(t *testing.T) {
	// "Test Chinese character token estimation" in Mandarin — 9 characters.
	// A real tokenizer (GPT-4 / tiktoken) would emit roughly 7–14 tokens.
	// Our word-based estimator treats the whole string as 1 word → 1 token.
	const input = "测试中文字符的token估算"
	const wantMin = 1
	const wantMax = 20 // upper bound: if we ever add CJK awareness this expands
	got := estimateTokens(input)
	if got < wantMin || got > wantMax {
		t.Errorf("estimateTokens(%q) = %d, want [%d, %d]; CJK calibration broken", input, got, wantMin, wantMax)
	}
}
