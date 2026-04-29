package agent_test

import (
	"context"
	"testing"

	"github.com/yabanci/agentshield/agent"
)

func TestTrace_CreatedOnAsk(t *testing.T) {
	srv := mockOllama(t, false)
	defer srv.Close()

	a := agent.NewWithOllamaURL(srv.URL)
	resp, err := a.Ask(context.Background(), "test prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.TraceID == "" {
		t.Error("expected non-empty trace_id in response")
	}

	tr := a.GetTrace(resp.TraceID)
	if tr == nil {
		t.Fatalf("trace %s not found in store", resp.TraceID)
	}
	if tr.Prompt == "" {
		t.Error("trace prompt should be set")
	}
	if tr.TotalMS < 0 {
		t.Error("trace total_ms should be >= 0")
	}
	if len(tr.Steps) == 0 {
		t.Error("trace should have at least one step")
	}
}

func TestTrace_StepsRecordOutcomes(t *testing.T) {
	srv := mockOllama(t, false) // primary succeeds
	defer srv.Close()

	a := agent.NewWithOllamaURL(srv.URL)
	resp, _ := a.Ask(context.Background(), "hello")

	tr := a.GetTrace(resp.TraceID)
	if tr == nil {
		t.Fatal("trace not found")
	}

	// Should have primary step with success outcome
	found := false
	for _, step := range tr.Steps {
		if step.Tier == agent.TierPrimary && step.Outcome == agent.OutcomeSuccess {
			found = true
			if step.LatencyMS < 0 {
				t.Error("step latency should be >= 0")
			}
		}
	}
	if !found {
		t.Errorf("expected a successful primary step, got: %+v", tr.Steps)
	}
}

func TestTrace_FallbackStepRecordedOnTransportFailure(t *testing.T) {
	srv := mockOllama(t, true) // primary always fails
	defer srv.Close()

	a := agent.NewWithOllamaURL(srv.URL)
	resp, _ := a.Ask(context.Background(), "test")

	tr := a.GetTrace(resp.TraceID)
	if tr == nil {
		t.Fatal("trace not found")
	}

	hasPrimaryFail := false
	hasFallbackSuccess := false
	for _, step := range tr.Steps {
		if step.Tier == agent.TierPrimary && step.Outcome == agent.OutcomeTransportError {
			hasPrimaryFail = true
		}
		if step.Tier == agent.TierFallback && step.Outcome == agent.OutcomeSuccess {
			hasFallbackSuccess = true
		}
	}
	if !hasPrimaryFail {
		t.Error("expected primary transport error step")
	}
	if !hasFallbackSuccess {
		t.Error("expected fallback success step")
	}
}

func TestTrace_SemanticFailureRecorded(t *testing.T) {
	srv := mockOllama(t, false)
	defer srv.Close()

	a := agent.NewWithOllamaURL(srv.URL)
	a.EnableDegradeMode() // primary returns garbage

	resp, _ := a.Ask(context.Background(), "test semantic")
	tr := a.GetTrace(resp.TraceID)
	if tr == nil {
		t.Fatal("trace not found")
	}

	// There should be a primary step with quality info
	hasPrimaryWithQuality := false
	for _, step := range tr.Steps {
		if step.Tier == agent.TierPrimary && step.QualityScore != nil {
			hasPrimaryWithQuality = true
			if *step.QualityScore > 0.45 {
				t.Errorf("degraded response quality should be <= 0.45, got %.2f", *step.QualityScore)
			}
		}
	}
	if !hasPrimaryWithQuality {
		t.Error("expected primary step with quality score")
	}
}

func TestTrace_MissingIDReturnsNil(t *testing.T) {
	srv := mockOllama(t, false)
	defer srv.Close()

	a := agent.NewWithOllamaURL(srv.URL)
	if tr := a.GetTrace("nonexistent"); tr != nil {
		t.Error("expected nil for missing trace ID")
	}
}
