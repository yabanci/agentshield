package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/yabanci/agentshield/memory"
)

const maxReactIterations = 6

// ReactStep is one iteration of the Thought → Action → Observation loop.
type ReactStep struct {
	Iteration   int            `json:"iteration"`
	Thought     string         `json:"thought,omitempty"`
	Action      string         `json:"action,omitempty"`
	ActionInput map[string]any `json:"action_input,omitempty"`
	Observation string         `json:"observation,omitempty"`
	Answer      string         `json:"answer,omitempty"`
}

// ReactResponse is the final output of a ReAct run.
type ReactResponse struct {
	Answer    string      `json:"answer"`
	Steps     []ReactStep `json:"steps"`
	Tier      Tier        `json:"tier"`
	Turns     int         `json:"turns"`
	SessionID string      `json:"session_id,omitempty"`
	TraceID   string      `json:"trace_id,omitempty"`
}

// React runs the ReAct (Reason + Act) agent loop.
// Each LLM call goes through the degradation chain.
// Each tool call has its own circuit breaker via ToolRegistry.
func (a *Agent) React(ctx context.Context, prompt, sessionID string) (ReactResponse, error) {
	tools := a.tools
	a.memory.Sessions.GetOrCreate(sessionID) // ensure session exists

	// Copy messages under lock before building history to avoid a data race
	// if concurrent requests share the same session ID.
	history := buildHistory(a.memory.Sessions.Messages(sessionID))

	// Full prompt = system + history + current user question
	fullPrompt := tools.SystemPrompt() + "\n\n" + history + "User: " + prompt + "\nAssistant:"

	var steps []ReactStep
	lastTier := TierPrimary
	conversationCtx := fullPrompt
	tr := a.memory.Traces.New(prompt)

	// done wraps every return path: finalizes the trace and sets TraceID.
	done := func(answer string, turns int, tier Tier) ReactResponse {
		a.memory.Sessions.Add(sessionID, memory.Message{Role: "assistant", Content: answer, Tier: tier, At: nowFn()})
		tr.Finalize(tier)
		return ReactResponse{
			Answer:    answer,
			Steps:     steps,
			Tier:      tier,
			Turns:     turns,
			SessionID: sessionID,
			TraceID:   tr.ID,
		}
	}

	a.memory.Sessions.Add(sessionID, memory.Message{Role: "user", Content: prompt, At: nowFn()})

	for i := 0; i < maxReactIterations; i++ {
		resp := a.degrade(ctx, conversationCtx, tr)
		lastTier = resp.Tier
		raw := strings.TrimSpace(resp.Text)

		step := parseReactStep(raw, i+1)
		steps = append(steps, step)

		if step.Answer != "" {
			return done(step.Answer, i+1, lastTier), nil
		}

		if step.Action != "" {
			obs, toolErr := tools.Execute(ctx, step.Action, step.ActionInput)
			if toolErr != nil {
				obs = fmt.Sprintf("Tool error: %v", toolErr)
			}
			step.Observation = obs
			steps[len(steps)-1].Observation = obs
			conversationCtx += fmt.Sprintf(
				"Thought: %s\nAction: %s\nActionInput: %s\nObservation: %s\n",
				step.Thought, step.Action, marshalArgs(step.ActionInput), obs,
			)
			continue
		}

		steps = append(steps, ReactStep{Iteration: i + 1, Answer: raw})
		return done(raw, i+1, lastTier), nil
	}

	last := "I was unable to complete the reasoning chain within the allowed iterations."
	if len(steps) > 0 && steps[len(steps)-1].Thought != "" {
		last = steps[len(steps)-1].Thought
	}
	return done(last, maxReactIterations, lastTier), nil
}

// parseReactStep parses one LLM output into a ReactStep.
// Handles variations in formatting that LLMs tend to produce. Never
// fails — if the LLM ignored the format entirely, the whole response
// becomes the Answer (see the bottom of this function).
func parseReactStep(raw string, iteration int) ReactStep {
	step := ReactStep{Iteration: iteration}
	lines := strings.Split(raw, "\n")

	for i, line := range lines {
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)

		if strings.HasPrefix(lower, "thought:") {
			step.Thought = strings.TrimSpace(line[8:])
		} else if strings.HasPrefix(lower, "answer:") {
			step.Answer = strings.TrimSpace(line[7:])
			// Collect multi-line answer
			for j := i + 1; j < len(lines); j++ {
				next := strings.TrimSpace(lines[j])
				if next != "" {
					step.Answer += " " + next
				}
			}
			return step
		} else if strings.HasPrefix(lower, "action:") && !strings.HasPrefix(lower, "action input") {
			step.Action = strings.TrimSpace(line[7:])
			step.Action = strings.Trim(step.Action, "`\"'")
		} else if strings.HasPrefix(lower, "actioninput:") || strings.HasPrefix(lower, "action input:") {
			jsonStr := extractAfterColon(line)
			if args, err := parseJSONArgs(jsonStr); err == nil {
				step.ActionInput = args
			} else {
				// Try wrapping bare value
				step.ActionInput = map[string]any{"value": jsonStr}
			}
		}
	}

	if step.Action == "" && step.Answer == "" {
		// Whole response is the answer (LLM skipped format)
		step.Answer = raw
	}
	return step
}

func extractAfterColon(line string) string {
	idx := strings.Index(line, ":")
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(line[idx+1:])
}

func parseJSONArgs(s string) (map[string]any, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "{}" {
		return map[string]any{}, nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		// Try adding quotes for bare string value
		wrapped := fmt.Sprintf(`{"value": %q}`, s)
		if err2 := json.Unmarshal([]byte(wrapped), &m); err2 != nil {
			return nil, err
		}
	}
	return m, nil
}

func marshalArgs(args map[string]any) string {
	b, err := json.Marshal(args)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func buildHistory(messages []memory.Message) string {
	if len(messages) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Previous conversation:\n")
	for _, m := range messages {
		switch m.Role {
		case "user":
			b.WriteString("User: " + m.Content + "\n")
		case "assistant":
			b.WriteString("Assistant: " + m.Content + "\n")
		}
	}
	b.WriteString("\n")
	return b.String()
}

// nowFn is overridable in tests.
var nowFn = time.Now //nolint:gochecknoglobals
