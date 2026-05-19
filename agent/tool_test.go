package agent_test

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/yabanci/agentshield/agent"
)

// ─── Expression evaluator ─────────────────────────────────────────────────

func TestCalculateTool(t *testing.T) {
	tool := &agent.ExposedCalculateTool{}
	ctx := context.Background()

	cases := []struct {
		expr string
		want float64
	}{
		{"2 + 3", 5},
		{"10 - 4", 6},
		{"3 * 4", 12},
		{"10 / 4", 2.5},
		{"2^10", 1024},
		{"2**10", 1024},
		{"(2 + 3) * 4", 20},
		{"10 / (2 + 3)", 2},
		{"-5 + 10", 5},
		{"sqrt(16)", 4},
		{"abs(-7)", 7},
		{"2^3^2", 512}, // right-associative: 2^(3^2) = 2^9 = 512
		{"3.14 * 2", 6.28},
	}

	for _, c := range cases {
		t.Run(c.expr, func(t *testing.T) {
			result, err := tool.Execute(ctx, map[string]any{"expression": c.expr})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got, _ := parseFloat(result)
			if math.Abs(got-c.want) > 0.0001 {
				t.Errorf("eval(%q) = %v, want %v", c.expr, got, c.want)
			}
		})
	}
}

func TestCalculateTool_Errors(t *testing.T) {
	tool := &agent.ExposedCalculateTool{}
	ctx := context.Background()

	_, err := tool.Execute(ctx, map[string]any{"expression": "10 / 0"})
	if err == nil {
		t.Error("expected division by zero error")
	}

	_, err = tool.Execute(ctx, map[string]any{})
	if err == nil {
		t.Error("expected error for empty expression")
	}
}

// TestCalculateTool_RejectsTrailingGarbage is the regression test for the
// round-3 finding. Pre-fix, `evalExpr("2+3 BOGUS")` returned 5.0 with no
// error because parseExpr stopped at the first non-operator token and
// the caller never checked whether the input was fully consumed.
func TestCalculateTool_RejectsTrailingGarbage(t *testing.T) {
	tool := &agent.ExposedCalculateTool{}
	ctx := context.Background()
	cases := []string{
		"2+3 BOGUS",
		"4*5 garbage",
		"sqrt(9) extra",
	}
	for _, expr := range cases {
		_, err := tool.Execute(ctx, map[string]any{"expression": expr})
		if err == nil {
			t.Errorf("expression %q should error on trailing garbage, got success", expr)
		}
	}
}

func TestGetTimeTool(t *testing.T) {
	tool := &agent.ExposedGetTimeTool{}
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{"timezone": "UTC"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty time result")
	}

	_, err = tool.Execute(ctx, map[string]any{"timezone": "Invalid/Zone"})
	if err == nil {
		t.Error("expected error for invalid timezone")
	}

	// Empty timezone defaults to UTC — should not error
	_, err = tool.Execute(ctx, map[string]any{})
	if err != nil {
		t.Errorf("empty timezone should default to UTC, got error: %v", err)
	}
}

func TestSearchDocsTool(t *testing.T) {
	tool := &agent.ExposedSearchDocsTool{}
	ctx := context.Background()

	cases := []struct {
		query   string
		wantKW  string
	}{
		{"circuit breaker", "Circuit Breaker"},
		{"retry backoff", "Retry"},
		{"hedge requests", "Hedged Requests"},
		{"bulkhead isolation", "Bulkhead"},
		{"load shedding", "Load Shedding"},
		{"semantic cache", "Semantic Cache"},
	}

	for _, c := range cases {
		t.Run(c.query, func(t *testing.T) {
			result, err := tool.Execute(ctx, map[string]any{"query": c.query})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !containsCI(result, c.wantKW) {
				t.Errorf("search(%q): expected %q in result, got: %s", c.query, c.wantKW, result[:min(100, len(result))])
			}
		})
	}

	// Empty query
	_, err := tool.Execute(ctx, map[string]any{})
	if err == nil {
		t.Error("expected error for empty query")
	}
}

func TestCheckSystemTool(t *testing.T) {
	srv := mockOllama(t, false)
	defer srv.Close()

	a := agent.NewWithOllamaURL(srv.URL)
	// CheckSystemTool is wired to the agent internally — test via registry
	result, err := a.ExecTool(context.Background(), "check_system", map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty system status")
	}
	// Should mention circuit breaker state
	if !containsCI(result, "closed") {
		t.Errorf("expected 'closed' in status, got: %s", result)
	}
}

// helpers

func parseFloat(s string) (float64, error) {
	var f float64
	_, err := fmt.Sscanf(s, "%f", &f)
	return f, err
}

func containsCI(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
