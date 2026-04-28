package agent_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yabanci/agentshield/agent"
)

// mockReactOllama serves scripted responses for ReAct testing.
// responses is a queue: first call gets responses[0], etc.
func mockReactOllama(t *testing.T, responses []string) *httptest.Server {
	t.Helper()
	idx := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			w.WriteHeader(http.StatusOK)
		case "/api/embeddings":
			json.NewEncoder(w).Encode(map[string]any{"embedding": []float64{1, 0, 0}})
		case "/api/generate":
			resp := "Answer: I don't know."
			if idx < len(responses) {
				resp = responses[idx]
				idx++
			}
			json.NewEncoder(w).Encode(map[string]any{"response": resp, "done": true})
		}
	}))
}

func TestReact_DirectAnswer(t *testing.T) {
	srv := mockReactOllama(t, []string{
		"Thought: I know the answer.\nAnswer: The sky is blue.",
	})
	defer srv.Close()

	a := agent.NewWithOllamaURL(srv.URL)
	resp, err := a.React(context.Background(), "what color is the sky?", "test-sess-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(resp.Answer, "blue") {
		t.Errorf("expected 'blue' in answer, got: %s", resp.Answer)
	}
	if resp.Turns != 1 {
		t.Errorf("expected 1 turn, got %d", resp.Turns)
	}
	if resp.SessionID != "test-sess-1" {
		t.Errorf("expected session id test-sess-1, got %s", resp.SessionID)
	}
}

func TestReact_WithCalculateTool(t *testing.T) {
	srv := mockReactOllama(t, []string{
		// First turn: ask to calculate
		"Thought: I need to calculate this.\nAction: calculate\nActionInput: {\"expression\": \"6 * 7\"}",
		// Second turn: use observation to answer
		"Thought: The result is 42.\nAnswer: 6 times 7 equals 42.",
	})
	defer srv.Close()

	a := agent.NewWithOllamaURL(srv.URL)
	resp, err := a.React(context.Background(), "what is 6 times 7?", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Turns < 2 {
		t.Errorf("expected >= 2 turns for tool use, got %d", resp.Turns)
	}
	// Steps should include tool call and observation
	hasTool := false
	for _, s := range resp.Steps {
		if s.Action == "calculate" {
			hasTool = true
			if s.Observation == "" {
				t.Error("tool observation should be non-empty")
			}
		}
	}
	if !hasTool {
		t.Error("expected calculate tool call in steps")
	}
}

func TestReact_WithTimeTool(t *testing.T) {
	srv := mockReactOllama(t, []string{
		"Thought: Need to check time.\nAction: get_time\nActionInput: {\"timezone\": \"UTC\"}",
		"Thought: Got the time.\nAnswer: The current UTC time is provided above.",
	})
	defer srv.Close()

	a := agent.NewWithOllamaURL(srv.URL)
	resp, err := a.React(context.Background(), "what time is it in UTC?", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	hasTimeTool := false
	for _, s := range resp.Steps {
		if s.Action == "get_time" {
			hasTimeTool = true
			if s.Observation == "" {
				t.Error("time tool should return non-empty result")
			}
		}
	}
	if !hasTimeTool {
		t.Error("expected get_time tool in steps")
	}
}

func TestReact_UnknownTool_Recovers(t *testing.T) {
	srv := mockReactOllama(t, []string{
		// LLM calls a non-existent tool
		"Thought: I'll use a magic tool.\nAction: does_not_exist\nActionInput: {}",
		// Gets error in observation, then answers directly
		"Thought: That tool doesn't exist, I'll answer directly.\nAnswer: I cannot find that information.",
	})
	defer srv.Close()

	a := agent.NewWithOllamaURL(srv.URL)
	resp, err := a.React(context.Background(), "use magic tool", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should recover and produce an answer
	if resp.Answer == "" {
		t.Error("expected non-empty answer even after unknown tool")
	}
	// The observation should contain an error message
	for _, s := range resp.Steps {
		if s.Action == "does_not_exist" && !strings.Contains(s.Observation, "unknown tool") {
			t.Errorf("expected 'unknown tool' in observation, got: %s", s.Observation)
		}
	}
}

func TestReact_SessionHistory(t *testing.T) {
	srv := mockReactOllama(t, []string{
		"Thought: Simple.\nAnswer: First answer.",
		"Thought: Simple.\nAnswer: Second answer.",
	})
	defer srv.Close()

	a := agent.NewWithOllamaURL(srv.URL)
	sessID := "multi-turn"

	_, err := a.React(context.Background(), "first question", sessID)
	if err != nil {
		t.Fatalf("first turn: %v", err)
	}

	_, err = a.React(context.Background(), "second question", sessID)
	if err != nil {
		t.Fatalf("second turn: %v", err)
	}

	sess := a.GetSession(sessID)
	if sess == nil {
		t.Fatal("session should exist")
	}
	if len(sess.Messages) < 4 { // 2 user + 2 assistant
		t.Errorf("expected >= 4 messages in session, got %d", len(sess.Messages))
	}
}

func TestReact_MaxIterations(t *testing.T) {
	// LLM never produces an Answer — always requests tools
	responses := make([]string, 20)
	for i := range responses {
		responses[i] = "Thought: Still thinking.\nAction: calculate\nActionInput: {\"expression\": \"1+1\"}"
	}

	srv := mockReactOllama(t, responses)
	defer srv.Close()

	a := agent.NewWithOllamaURL(srv.URL)
	resp, err := a.React(context.Background(), "infinite loop question", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should stop at max iterations and return something
	if resp.Answer == "" {
		t.Error("should return non-empty answer even at max iterations")
	}
	if resp.Turns > 6 {
		t.Errorf("should stop at max 6 iterations, got %d", resp.Turns)
	}
}
