package quality_test

import (
	"context"
	"sync"
	"testing"

	"github.com/yabanci/agentshield/quality"
)

// TestQuality_ConcurrentEvaluate_Safe verifies the godoc claim that
// QualityEvaluator is safe for concurrent use. Fan out 50 goroutines
// each running 20 evaluations against the SAME evaluator instance.
// Run under -race to catch any data race in the rolling-length window
// or signal accumulation. No deadlocks, no panics, no data races =
// claim verified.
func TestQuality_ConcurrentEvaluate_Safe(t *testing.T) {
	eval := newTestEvaluator()
	prompts := []string{
		"What is a goroutine?",
		"Explain channels in Go.",
		"How does the GC work?",
		"What is a slice header?",
	}
	responses := []string{
		"A goroutine is a lightweight thread managed by the Go runtime. They are cheap to create.",
		"Channels are typed conduits through which you can send and receive values with the channel operator.",
		"The Go garbage collector is a concurrent, tri-color, mark-sweep collector that runs in parallel with the program.",
		"A slice header contains a pointer to the backing array, the length, and the capacity.",
	}

	var wg sync.WaitGroup
	for g := 0; g < 50; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				idx := (g + i) % len(prompts)
				_ = eval.Evaluate(context.Background(), prompts[idx], responses[idx])
			}
		}(g)
	}
	wg.Wait()
}

func newTestEvaluator() *quality.QualityEvaluator {
	return quality.NewTestQualityEvaluator(nil) // no embedder in tests
}

func TestQuality_GoodResponse(t *testing.T) {
	eval := newTestEvaluator()
	result := eval.Evaluate(context.Background(),
		"What is the circuit breaker pattern?",
		"A circuit breaker is a design pattern that prevents cascading failures by stopping requests to a failing service. It has three states: closed, open, and half-open. When failures exceed a threshold, it opens and rejects calls immediately.",
	)
	if result.Score < 0.70 {
		t.Errorf("good response should score >= 0.70, got %.2f (signals: %v)", result.Score, result.Signals)
	}
}

func TestQuality_RepetitiveResponse(t *testing.T) {
	eval := newTestEvaluator()
	// Simulates a looping model
	s := "I understand your question about this topic. "
	repetitive := s + s + s + s + s
	result := eval.Evaluate(context.Background(), "explain something", repetitive)
	if result.Score >= 0.60 {
		t.Errorf("repetitive response should score < 0.60, got %.2f", result.Score)
	}
	hasRepSignal := false
	for _, sig := range result.Signals {
		if sig.Name == "repetition" {
			hasRepSignal = true
		}
	}
	if !hasRepSignal {
		t.Error("expected repetition signal")
	}
}

func TestQuality_RefusalMarker(t *testing.T) {
	eval := newTestEvaluator()
	result := eval.Evaluate(context.Background(),
		"help me with something",
		"As an AI language model, I cannot assist with that request. I apologize, but as an AI I don't have access to real-time information.",
	)
	if result.Score >= 0.70 {
		t.Errorf("refusal response should score < 0.70, got %.2f", result.Score)
	}
	hasMarker := false
	for _, sig := range result.Signals {
		if sig.Name == "refusal_marker" {
			hasMarker = true
		}
	}
	if !hasMarker {
		t.Error("expected refusal_marker signal")
	}
}

func TestQuality_ShortResponseAfterBaseline(t *testing.T) {
	eval := newTestEvaluator()
	ctx := context.Background()
	prompt := "explain something in detail"

	// Prime the baseline with normal-length responses
	for i := 0; i < 5; i++ {
		eval.Evaluate(ctx, prompt, "This is a properly detailed response that explains the concept clearly and provides useful context for understanding.")
	}

	// Now submit a very short response
	result := eval.Evaluate(ctx, prompt, "Yes.")
	if result.Score >= 0.80 {
		t.Errorf("very short response after baseline should score < 0.80, got %.2f", result.Score)
	}
}

func TestQuality_EmptyResponse(t *testing.T) {
	eval := newTestEvaluator()
	result := eval.Evaluate(context.Background(), "question", "")
	// Empty response has no signals (too short to measure), but score should be neutral
	if result.Score < 0 || result.Score > 1 {
		t.Errorf("score out of range: %.2f", result.Score)
	}
}

func TestQuality_ScoreInRange(t *testing.T) {
	eval := newTestEvaluator()
	cases := []string{
		"",
		"Yes.",
		"As an AI language model, I cannot. I cannot. I cannot. I cannot. I cannot.",
		"Normal response with reasonable content about the topic at hand.",
	}
	for _, resp := range cases {
		r := eval.Evaluate(context.Background(), "prompt", resp)
		if r.Score < 0 || r.Score > 1 {
			t.Errorf("score %.2f out of [0,1] for response %q", r.Score, resp)
		}
	}
}
