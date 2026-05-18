package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// TestMCPLookupTool_BasicCall verifies the wire format: tool name + args
// in JSON body, result extracted from the {result: ...} envelope.
func TestMCPLookupTool_BasicCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mcp/call" {
			http.NotFound(w, r)
			return
		}
		var req struct {
			Tool string `json:"tool"`
			Args string `json:"args"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Tool != "weather" || req.Args != "Berlin" {
			t.Errorf("wire body wrong: tool=%q args=%q", req.Tool, req.Args)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"result": "Berlin: 18°C partly cloudy",
		})
	}))
	defer srv.Close()

	tool := NewMCPLookupTool(srv.URL)
	out, err := tool.Execute(context.Background(), map[string]any{
		"tool": "weather", "args": "Berlin",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, "18°C") {
		t.Errorf("result = %q, want Berlin weather", out)
	}
}

// TestMCPLookupTool_ServerError_PropagatesAsError ensures non-2xx
// responses bubble up as Go errors so the per-tool CB counts them.
func TestMCPLookupTool_ServerError_PropagatesAsError(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	tool := NewMCPLookupTool(srv.URL)
	_, err := tool.Execute(context.Background(), map[string]any{
		"tool": "weather", "args": "x",
	})
	if err == nil {
		t.Fatal("expected error from 503 response")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("error should mention status code, got %q", err.Error())
	}
	if calls.Load() != 1 {
		t.Errorf("expected exactly 1 server call, got %d", calls.Load())
	}
}

// TestMCPLookupTool_DisabledWhenURLEmpty: caller passes empty URL,
// tool returns an error immediately without attempting any HTTP work.
// This is the guard for "MCP_URL unset → tool absent" registry behavior.
func TestMCPLookupTool_DisabledWhenURLEmpty(t *testing.T) {
	tool := NewMCPLookupTool("")
	_, err := tool.Execute(context.Background(), map[string]any{"tool": "x", "args": ""})
	if err == nil || !strings.Contains(err.Error(), "MCP not configured") {
		t.Fatalf("expected MCP-not-configured error, got %v", err)
	}
}

func TestMCPLookupTool_MissingToolArg(t *testing.T) {
	tool := NewMCPLookupTool("http://localhost:0")
	_, err := tool.Execute(context.Background(), map[string]any{"args": "Berlin"})
	if err == nil {
		t.Fatal("expected error for missing tool arg")
	}
}

// TestMCPLookupTool_ContextCancellation: cancelling the context aborts
// the in-flight HTTP call. This is what lets the orchestrator unwind
// cleanly when a chat request times out.
func TestMCPLookupTool_ContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Hang forever — the test will cancel before this returns.
		<-r.Context().Done()
	}))
	defer srv.Close()

	tool := NewMCPLookupTool(srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel

	_, err := tool.Execute(ctx, map[string]any{"tool": "weather", "args": "x"})
	if err == nil {
		t.Fatal("expected ctx cancellation error")
	}
	// The error wraps context.Canceled (via http transport / fmt.Errorf).
	if !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "context") {
		t.Errorf("error should reflect context cancellation, got %q", err)
	}
}
