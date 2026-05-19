package agent_test

import (
	"context"
	"strings"
	"testing"

	dto "github.com/prometheus/client_model/go"
	"github.com/yabanci/agentshield/agent"
	"github.com/yabanci/agentshield/telemetry"
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

// TestSummarize_SingleIterationNotSummarized verifies that a transcript with
// exactly 4 lines (one complete ReAct iteration) is returned unchanged even
// when the threshold is set to 1 — the short-circuit guard must fire.
func TestSummarize_SingleIterationNotSummarized(t *testing.T) {
	a := agent.NewWithOllamaURL("http://127.0.0.1:1") // unreachable — must not be called
	transcript := "Thought: think\nAction: calc\nActionInput: {}\nObservation: 42"
	got := a.SummarizeTranscript(context.Background(), transcript, 1)
	if got != transcript {
		t.Errorf("4-line transcript should be returned unchanged, got: %q", got)
	}
}

// TestSummarizationsCounter_NotBumpedOnShortTranscript validates the B1 fix:
// the ReactSummarizationsTotal counter must NOT be incremented when the
// transcript is too short (≤4 lines) and the summarizer short-circuits.
func TestSummarizationsCounter_NotBumpedOnShortTranscript(t *testing.T) {
	a := agent.NewWithOllamaURL("http://127.0.0.1:1")

	// Snapshot the counter before the call.
	before := counterValue(t, telemetry.ReactSummarizationsTotal)

	// 4-line transcript + threshold=1 → short-circuit path, no real summarization.
	transcript := "Thought: think\nAction: calc\nActionInput: {}\nObservation: 42"
	a.SummarizeTranscript(context.Background(), transcript, 1)

	after := counterValue(t, telemetry.ReactSummarizationsTotal)
	if after != before {
		t.Errorf("counter bumped on short-circuit: before=%g after=%g", before, after)
	}
}

// counterValue reads the current value of a prometheus.Counter via Write.
func counterValue(t *testing.T, c interface {
	Write(*dto.Metric) error
}) float64 {
	t.Helper()
	m := &dto.Metric{}
	if err := c.Write(m); err != nil {
		t.Fatalf("failed to read counter: %v", err)
	}
	if m.Counter == nil {
		return 0
	}
	return m.Counter.GetValue()
}

// TestSummarize_HardTruncateGuarantee (round-14 follow-up) verifies the final
// shrink guarantee fires when both prior guards fail to reduce size. With many
// short Observation lines that all fit in abbreviate's 10 KB cap, the first two
// branches return a result roughly equal to the original; the third branch
// must hard-truncate so the result is strictly smaller.
func TestSummarize_HardTruncateGuarantee(t *testing.T) {
	a := agent.NewWithOllamaURL("http://127.0.0.1:1") // unreachable → fallback path
	transcript := strings.Repeat("Observation: x\n", 400)
	before := len(transcript)
	got := a.SummarizeTranscript(context.Background(), transcript, 1)
	if len(got) >= before {
		t.Errorf("hard-truncate guarantee broken: before=%d after=%d", before, len(got))
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
