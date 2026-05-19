package agent

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"go.opentelemetry.io/otel/attribute"

	"github.com/yabanci/agentshield/provider"
)

// maxAbbrevBytes caps the deterministic abbreviation path so a huge transcript
// cannot allocate unbounded memory when both LLM providers are unavailable.
const maxAbbrevBytes = 10 * 1024

// estimateTokens approximates the token count of s.
// Formula: word-count × 4/3 (English-ish assumption).
// Intentional under-estimate for non-Latin scripts; calibration is in tests.
// We do not ship a real tokenizer to keep the binary dependency-free.
func estimateTokens(s string) int {
	words := len(strings.Fields(s))
	// Integer arithmetic: multiply by 4, divide by 3.
	return (words * 4) / 3
}

// SummarizeTranscript is the exported test-surface entry point for the internal
// summarization logic. Production code inside react.go calls summarizeTranscript
// directly; tests use this to exercise the path without a running React loop.
func (a *Agent) SummarizeTranscript(ctx context.Context, transcript string, threshold int) string {
	return a.summarizeTranscript(ctx, transcript, threshold)
}

// summarizeTranscript reduces the running transcript when it would exceed the
// configured token threshold. It keeps the most-recent iteration intact (the
// LLM needs that to decide the next action) and replaces the older half with
// a single-paragraph LLM-generated summary or a deterministic abbreviation.
//
// The span is started as a child of iterCtx so the trace tree reads:
//
//	agentshield.react.iteration
//	  └── agentshield.react.summarize
func (a *Agent) summarizeTranscript(iterCtx context.Context, transcript string, threshold int) string {
	tokens := estimateTokens(transcript)
	if tokens < threshold {
		return transcript
	}

	// Split into lines; keep the last "iteration's worth" (at minimum the last
	// 4 lines: Thought / Action / ActionInput / Observation).
	lines := strings.Split(transcript, "\n")
	if len(lines) <= 4 {
		// Transcript is so short that splitting would lose context — return as-is
		// even though it technically exceeds the threshold. Prevents thrashing on
		// degenerate inputs.
		return transcript
	}

	// Keep the most-recent iteration intact.
	pivot := len(lines) / 2
	older := strings.Join(lines[:pivot], "\n")
	recent := strings.Join(lines[pivot:], "\n")

	beforeTokens := tokens
	ctx, span := reactTracer.Start(iterCtx, "agentshield.react.summarize")
	defer span.End()
	span.SetAttributes(attribute.Int("before.tokens", beforeTokens))

	summary, fallback := a.callSummarizeLLM(ctx, older)
	span.SetAttributes(attribute.Bool("fallback", fallback))

	result := summary + "\n" + recent
	span.SetAttributes(attribute.Int("after.tokens", estimateTokens(result)))
	return result
}

// callSummarizeLLM asks the fallback model to summarise older with a tight
// system prompt. Returns the summary and a flag indicating whether the
// deterministic abbreviation path was used (true = provider unavailable).
func (a *Agent) callSummarizeLLM(ctx context.Context, older string) (string, bool) {
	const sysPrompt = "You are a concise summarizer. Summarize the following conversation history in one paragraph of at most 200 tokens. Preserve key facts, tool results, and decision points. Do not add commentary."

	resp, err := a.fallback.Generate(ctx, provider.Request{
		Model:     a.fallbackModel,
		System:    sysPrompt,
		Prompt:    older,
		MaxTokens: 250,
	})
	if err == nil && strings.TrimSpace(resp.Text) != "" {
		return "[Summary of prior context]: " + strings.TrimSpace(resp.Text), false
	}

	// Both providers unavailable — deterministic abbreviation.
	// Take the first 100 chars of each Observation line, cap total at 10 KB.
	return abbreviate(older), true
}

// abbreviate produces a deterministic transcript reduction when the LLM is
// unavailable. It extracts Observation lines and caps total output at
// maxAbbrevBytes to avoid unbounded allocation.
func abbreviate(older string) string {
	const obsPrefix = "observation:"
	const charLimit = 100

	var parts []string
	totalBytes := 0
	for _, line := range strings.Split(older, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(strings.ToLower(trimmed), obsPrefix) {
			continue
		}
		excerpt := trimmed
		if utf8.RuneCountInString(excerpt) > charLimit {
			// Truncate at charLimit runes, not bytes, to handle multi-byte chars.
			runes := []rune(excerpt)
			excerpt = string(runes[:charLimit]) + "…"
		}
		needed := len(excerpt)
		if totalBytes+needed > maxAbbrevBytes {
			break
		}
		parts = append(parts, excerpt)
		totalBytes += needed
	}

	if len(parts) == 0 {
		return fmt.Sprintf("[%d prior lines abbreviated]", len(strings.Split(older, "\n")))
	}
	return "[Prior observations]: " + strings.Join(parts, "; ")
}
