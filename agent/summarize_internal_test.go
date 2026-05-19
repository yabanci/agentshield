package agent

import "testing"

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
