package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/yabanci/agentshield/memory"
	"github.com/yabanci/agentshield/telemetry"
)

// toolCacheEnabled returns true when the agent config enables the per-session
// tool cache (allows nil-safe access in tests that don't go through LoadFromEnv).
func (a *Agent) toolCacheEnabled() bool {
	if a.cfg == nil {
		return false
	}
	return a.cfg.ToolCache.Enabled
}

// toolCacheMaxEntries returns the per-session LRU cap from config, or the
// default of 64 when config is absent.
func (a *Agent) toolCacheMaxEntries() int {
	if a.cfg == nil || a.cfg.ToolCache.MaxEntries <= 0 {
		return 64
	}
	return a.cfg.ToolCache.MaxEntries
}

// reactMaxTranscriptTokens returns the summarization threshold from config,
// or the default of 6000 when config is absent.
func (a *Agent) reactMaxTranscriptTokens() int {
	if a.cfg == nil || a.cfg.ReAct.MaxTranscriptTokens <= 0 {
		return 6000
	}
	return a.cfg.ReAct.MaxTranscriptTokens
}

const maxReactIterations = 6

// reactTracer is the named OTel tracer for ReAct agent spans.
var reactTracer = telemetry.Tracer("agentshield/react") //nolint:gochecknoglobals // package-level tracer per OTel idiom

// maxAttrBytes is the per-attribute truncation limit for tool input/output.
// OTel SDK rejects attribute values over 64 KB; we cap at 2 KB to keep traces
// readable and avoid inflating the payload for large tool responses.
const maxAttrBytes = 2 * 1024

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

	// Per-session tool result cache — discarded when this React call returns.
	tc := newToolCache(a.toolCacheMaxEntries(), a.toolCacheEnabled())
	threshold := a.reactMaxTranscriptTokens()

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
		iterCtx, iterSpan := reactTracer.Start(ctx, "agentshield.react.iteration")
		iterSpan.SetAttributes(attribute.Int("iteration", i+1))

		// Part B: transcript token accounting + optional summarization.
		tokens := estimateTokens(conversationCtx)
		telemetry.ReactTranscriptTokens.Observe(float64(tokens))
		if tokens >= threshold {
			conversationCtx = a.summarizeTranscript(iterCtx, conversationCtx, threshold)
		}

		resp := a.degrade(iterCtx, conversationCtx, tr)
		lastTier = resp.Tier
		raw := strings.TrimSpace(resp.Text)

		step := parseReactStep(raw, i+1)
		steps = append(steps, step)

		if step.Answer != "" {
			iterSpan.End()
			return done(step.Answer, i+1, lastTier), nil
		}

		if step.Action != "" {
			toolCtx, toolSpan := reactTracer.Start(iterCtx, "agentshield.tool."+step.Action)
			// Truncate tool.input to 2 KB; OTel SDK rejects values over 64 KB
			// and large inputs waste trace storage without adding signal.
			inputStr := truncateAttr(marshalArgs(step.ActionInput))
			toolSpan.SetAttributes(attribute.String("tool.input", inputStr))

			// Part A: check per-session tool cache before calling the tool.
			// Sanitize the tool label to the known registry to prevent unbounded
			// Prometheus cardinality when the LLM emits an unrecognised tool name.
			knownTools := tools.KnownNames()
			canonicalTool := "unknown"
			if _, ok := knownTools[strings.ToLower(step.Action)]; ok {
				canonicalTool = strings.ToLower(step.Action)
			}
			var obs string
			if cached, hit := tc.Get(step.Action, inputStr); hit {
				obs = cached
				toolSpan.SetAttributes(attribute.Bool("tool.cache.hit", true))
				telemetry.ToolCacheHitsTotal.WithLabelValues(canonicalTool).Inc()
			} else {
				toolSpan.SetAttributes(attribute.Bool("tool.cache.hit", false))
				telemetry.ToolCacheMissesTotal.WithLabelValues(canonicalTool).Inc()
				var toolErr error
				obs, toolErr = tools.Execute(toolCtx, step.Action, step.ActionInput)
				if toolErr != nil {
					toolSpan.RecordError(toolErr)
					toolSpan.SetStatus(codes.Error, toolErr.Error())
					obs = fmt.Sprintf("Tool error: %v", toolErr)
				}
				// Only cache successful (non-error) results.
				if toolErr == nil {
					tc.Set(step.Action, inputStr, obs)
				}
			}

			toolSpan.SetAttributes(attribute.Int("tool.output.len", len(obs)))
			toolSpan.End()

			step.Observation = obs
			steps[len(steps)-1].Observation = obs
			conversationCtx += fmt.Sprintf(
				"Thought: %s\nAction: %s\nActionInput: %s\nObservation: %s\n",
				step.Thought, step.Action, inputStr, obs,
			)
			iterSpan.End()
			continue
		}

		iterSpan.End()
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

// truncateAttr caps a string at maxAttrBytes for OTel span attributes.
// Truncated strings are suffixed with "...[truncated]" so consumers know data
// was cut. See maxAttrBytes for the rationale.
func truncateAttr(s string) string {
	if len(s) <= maxAttrBytes {
		return s
	}
	return s[:maxAttrBytes] + "...[truncated]"
}

// nowFn is overridable in tests.
var nowFn = time.Now //nolint:gochecknoglobals
