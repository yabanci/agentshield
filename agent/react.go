package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
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
}

// React runs the ReAct (Reason + Act) agent loop.
// Each LLM call goes through the degradation chain.
// Each tool call has its own circuit breaker via ToolRegistry.
func (a *Agent) React(ctx context.Context, prompt, sessionID string) (ReactResponse, error) {
	tools := a.tools
	session := a.sessions.GetOrCreate(sessionID)

	// Build conversation history for context
	history := buildHistory(session.Messages)

	// Full prompt = system + history + current user question
	fullPrompt := tools.SystemPrompt() + "\n\n" + history + "User: " + prompt + "\nAssistant:"

	var steps []ReactStep
	lastTier := TierPrimary
	conversationCtx := fullPrompt
	tr := a.traces.New(prompt) // React has its own trace

	// Record user message
	a.sessions.Add(sessionID, Message{Role: "user", Content: prompt, At: nowFn()})

	for i := 0; i < maxReactIterations; i++ {
		// LLM call through the resilience chain
		resp, err := a.degrade(ctx, conversationCtx, tr)
		if err != nil {
			return ReactResponse{}, fmt.Errorf("react llm call failed: %w", err)
		}
		lastTier = resp.Tier
		raw := strings.TrimSpace(resp.Text)

		step, err := parseReactStep(raw, i+1)
		if err != nil {
			// LLM gave unparseable output — treat as final answer
			steps = append(steps, ReactStep{Iteration: i + 1, Answer: raw})
			a.sessions.Add(sessionID, Message{Role: "assistant", Content: raw, Tier: lastTier, At: nowFn()})
			return ReactResponse{
				Answer:    raw,
				Steps:     steps,
				Tier:      lastTier,
				Turns:     i + 1,
				SessionID: sessionID,
			}, nil
		}

		steps = append(steps, step)

		// Final answer — done
		if step.Answer != "" {
			a.sessions.Add(sessionID, Message{Role: "assistant", Content: step.Answer, Tier: lastTier, At: nowFn()})
			return ReactResponse{
				Answer:    step.Answer,
				Steps:     steps,
				Tier:      lastTier,
				Turns:     i + 1,
				SessionID: sessionID,
			}, nil
		}

		// Tool call
		if step.Action != "" {
			obs, toolErr := tools.Execute(ctx, step.Action, step.ActionInput)
			if toolErr != nil {
				obs = fmt.Sprintf("Tool error: %v", toolErr)
			}
			step.Observation = obs
			steps[len(steps)-1].Observation = obs

			// Append to conversation context
			conversationCtx += fmt.Sprintf(
				"Thought: %s\nAction: %s\nActionInput: %s\nObservation: %s\n",
				step.Thought, step.Action, marshalArgs(step.ActionInput), obs,
			)
			continue
		}

		// Neither answer nor action — force finish
		steps = append(steps, ReactStep{Iteration: i + 2, Answer: raw})
		a.sessions.Add(sessionID, Message{Role: "assistant", Content: raw, Tier: lastTier, At: nowFn()})
		return ReactResponse{
			Answer: raw, Steps: steps, Tier: lastTier, Turns: i + 1, SessionID: sessionID,
		}, nil
	}

	// Max iterations hit — return last thought as answer
	last := "I was unable to complete the reasoning chain within the allowed iterations."
	if len(steps) > 0 && steps[len(steps)-1].Thought != "" {
		last = steps[len(steps)-1].Thought
	}
	a.sessions.Add(sessionID, Message{Role: "assistant", Content: last, Tier: lastTier, At: nowFn()})
	return ReactResponse{
		Answer: last, Steps: steps, Tier: lastTier, Turns: maxReactIterations, SessionID: sessionID,
	}, nil
}

// parseReactStep parses one LLM output into a ReactStep.
// Handles variations in formatting that LLMs tend to produce.
func parseReactStep(raw string, iteration int) (ReactStep, error) {
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
			return step, nil
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
	return step, nil
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

func buildHistory(messages []Message) string {
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
var nowFn = func() time.Time { return time.Now() } //nolint:gochecknoglobals
