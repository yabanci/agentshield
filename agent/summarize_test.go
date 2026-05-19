package agent_test

import (
	"context"
	"strings"
	"testing"

	"github.com/yabanci/agentshield/agent"
)

// TestEstimateTokens is tested indirectly via the summarization path.
// Direct unit test lives in summarize_internal_test.go.

// TestSummarize_BelowThreshold verifies that a short transcript is returned
// unchanged — no LLM call is made.
func TestSummarize_BelowThreshold(t *testing.T) {
	a := agent.NewWithOllamaURL("http://127.0.0.1:1") // unreachable — must not be called
	transcript := "Thought: simple\nAction: calc\nActionInput: {}\nObservation: 42"
	got := a.SummarizeTranscript(context.Background(), transcript, 9999)
	if got != transcript {
		t.Errorf("expected unchanged transcript below threshold, got: %q", got)
	}
}

// TestSummarize_TriggersAtThreshold verifies the transcript shrinks when the
// threshold is exceeded and the LLM is available.
func TestSummarize_TriggersAtThreshold(t *testing.T) {
	srv := mockReactOllama(t, []string{
		"This is the summary.",
	})
	defer srv.Close()

	a := agent.NewWithOllamaURL(srv.URL)
	transcript := strings.Repeat("Thought: keep thinking\nAction: calc\nActionInput: {\"e\":\"1+1\"}\nObservation: 2\n", 5)
	original := len(transcript)
	got := a.SummarizeTranscript(context.Background(), transcript, 1)

	if len(got) >= original {
		t.Errorf("expected shorter transcript after summarization, original=%d got=%d", original, len(got))
	}
	hasSummaryMarker := strings.Contains(got, "This is the summary.") ||
		strings.Contains(got, "[Prior observations]") ||
		strings.Contains(got, "[Summary of prior context]")
	if !hasSummaryMarker {
		t.Errorf("expected summary marker in output, got: %q", got)
	}
}

// TestSummarize_DeterministicFallback verifies the abbreviation path fires
// when the LLM provider is unavailable.
func TestSummarize_DeterministicFallback(t *testing.T) {
	a := agent.NewWithOllamaURL("http://127.0.0.1:1") // unreachable

	transcript := strings.Repeat("Thought: x\nAction: y\nActionInput: {}\nObservation: result-value\n", 10)
	got := a.SummarizeTranscript(context.Background(), transcript, 1)

	// Result must be smaller than the input.
	if len(got) >= len(transcript) {
		t.Errorf("expected reduced transcript, original=%d got=%d", len(transcript), len(got))
	}
}

// TestSummarize_RecentIterationPreserved verifies the most-recent iteration
// is never overwritten by the summary.
func TestSummarize_RecentIterationPreserved(t *testing.T) {
	a := agent.NewWithOllamaURL("http://127.0.0.1:1") // unreachable → abbrev path

	marker := "UNIQUE_RECENT_MARKER_XYZ"
	transcript := strings.Repeat("Thought: old\nObservation: old-obs\n", 8) +
		"Thought: " + marker + "\nAction: calc\nObservation: latest\n"
	got := a.SummarizeTranscript(context.Background(), transcript, 1)

	if !strings.Contains(got, marker) {
		t.Errorf("most-recent marker %q not found in summarized transcript: %q", marker, got)
	}
}
