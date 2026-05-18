package agent

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/yabanci/flowguard/circuitbreaker"
)

// Tool is a callable capability the agent can invoke during a ReAct loop.
type Tool interface {
	Name() string
	Description() string
	ArgsSchema() string
	Execute(ctx context.Context, args map[string]any) (string, error)
}

// defaultToolTimeout caps per-tool execution time.
// A misbehaving tool can otherwise hang the entire ReAct loop until the
// parent context expires.
const defaultToolTimeout = 10 * time.Second

// ToolRegistry holds tools and wraps each with a circuit breaker.
//
// Both maps are populated once during newToolRegistry and never mutated
// after that, so they are safe for concurrent reads without a lock.
type ToolRegistry struct {
	tools map[string]Tool
	cbs   map[string]*circuitbreaker.Breaker
}

func newToolRegistry(a *Agent) *ToolRegistry {
	r := &ToolRegistry{
		tools: make(map[string]Tool),
		cbs:   make(map[string]*circuitbreaker.Breaker),
	}
	r.register(&CalculateTool{})
	r.register(&GetTimeTool{})
	r.register(&SearchDocsTool{})
	r.register(&CheckSystemTool{agent: a})
	return r
}

func (r *ToolRegistry) register(t Tool) {
	r.tools[t.Name()] = t
	r.cbs[t.Name()] = circuitbreaker.New(
		circuitbreaker.WithFailureThreshold(3),
		circuitbreaker.WithOpenTimeout(20*time.Second),
	)
}

func (r *ToolRegistry) Execute(ctx context.Context, name string, args map[string]any) (string, error) {
	t, ok := r.tools[name]
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", name)
	}
	// Per-tool timeout — protects against hangs in tool implementations.
	tctx, cancel := context.WithTimeout(ctx, defaultToolTimeout)
	defer cancel()

	cb := r.cbs[name]
	var result string
	err := cb.Do(tctx, func(ctx context.Context) error {
		var execErr error
		result, execErr = t.Execute(ctx, args)
		return execErr
	})
	return result, err
}

func (r *ToolRegistry) SystemPrompt() string {
	var b strings.Builder
	b.WriteString("You are a helpful AI assistant with access to tools.\n\n")
	b.WriteString("Available tools:\n")
	for _, t := range r.tools {
		fmt.Fprintf(&b, "- %s: %s. Args: %s\n", t.Name(), t.Description(), t.ArgsSchema())
	}
	b.WriteString(`
When you need to use a tool, respond EXACTLY like this (no extra text before):
Thought: <your reasoning>
Action: <tool_name>
ActionInput: {"key": "value"}

When you have the final answer, respond EXACTLY like this:
Thought: <final reasoning>
Answer: <your complete answer>

Never mix formats. Use tools when they help give a precise answer.`)
	return b.String()
}

func (r *ToolRegistry) List() []map[string]string {
	list := make([]map[string]string, 0, len(r.tools))
	for _, t := range r.tools {
		list = append(list, map[string]string{
			"name":        t.Name(),
			"description": t.Description(),
		})
	}
	return list
}

// ─── CalculateTool ─────────────────────────────────────────────────────────

type CalculateTool struct{}

func (t *CalculateTool) Name() string        { return "calculate" }
func (t *CalculateTool) Description() string { return "Evaluate a mathematical expression" }
func (t *CalculateTool) ArgsSchema() string  { return `{"expression": "2^10 + 5 * 3"}` }

func (t *CalculateTool) Execute(_ context.Context, args map[string]any) (string, error) {
	expr, _ := args["expression"].(string)
	if expr == "" {
		return "", fmt.Errorf("expression is required")
	}
	result, err := evalExpr(expr)
	if err != nil {
		return "", fmt.Errorf("cannot evaluate %q: %w", expr, err)
	}
	if result == math.Trunc(result) {
		return fmt.Sprintf("%.0f", result), nil
	}
	return strconv.FormatFloat(result, 'f', 6, 64), nil
}

// ─── GetTimeTool ───────────────────────────────────────────────────────────

type GetTimeTool struct{}

func (t *GetTimeTool) Name() string        { return "get_time" }
func (t *GetTimeTool) Description() string { return "Get the current date and time in a timezone" }
func (t *GetTimeTool) ArgsSchema() string  { return `{"timezone": "Asia/Almaty"}` }

func (t *GetTimeTool) Execute(_ context.Context, args map[string]any) (string, error) {
	tz, _ := args["timezone"].(string)
	if tz == "" {
		tz = "UTC"
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return "", fmt.Errorf("unknown timezone %q: %w", tz, err)
	}
	now := time.Now().In(loc)
	return fmt.Sprintf("%s (timezone: %s)", now.Format("Monday, 02 January 2006 15:04:05"), tz), nil
}

// ─── SearchDocsTool ────────────────────────────────────────────────────────

type SearchDocsTool struct{}

func (t *SearchDocsTool) Name() string        { return "search_docs" }
func (t *SearchDocsTool) Description() string { return "Search the resilience engineering knowledge base" }
func (t *SearchDocsTool) ArgsSchema() string  { return `{"query": "circuit breaker pattern"}` }

func (t *SearchDocsTool) Execute(_ context.Context, args map[string]any) (string, error) {
	query, _ := args["query"].(string)
	if query == "" {
		return "", fmt.Errorf("query is required")
	}
	return searchKB(query), nil
}

// ─── CheckSystemTool ───────────────────────────────────────────────────────

type CheckSystemTool struct{ agent *Agent }

func (t *CheckSystemTool) Name() string        { return "check_system" }
func (t *CheckSystemTool) Description() string { return "Check AgentShield's current resilience status" }
func (t *CheckSystemTool) ArgsSchema() string  { return `{}` }

func (t *CheckSystemTool) Execute(_ context.Context, _ map[string]any) (string, error) {
	s := t.agent.Status()
	return fmt.Sprintf(
		"Primary circuit breaker: %s | Fallback circuit breaker: %s | "+
			"Cache entries: %d | Total requests: %d | Primary error rate: %.0f%% | "+
			"Load shedder limit: %d (in-flight: %d)",
		s.PrimaryBreaker, s.FallbackBreaker,
		s.CacheSize, s.TotalRequests, s.ErrorRate*100,
		s.LoadshedLimit, s.LoadshedInflight,
	), nil
}

// ─── Expression evaluator (recursive descent) ──────────────────────────────

type exprParser struct {
	input []rune
	pos   int
	err   error
}

func evalExpr(s string) (float64, error) {
	p := &exprParser{input: []rune(strings.ReplaceAll(s, "**", "^"))}
	p.skipWS()
	v := p.parseExpr()
	if p.err != nil {
		return 0, p.err
	}
	// Reject trailing garbage: "2+3 BOGUS" used to return 5.0 with no
	// error because parseExpr stopped at the first non-operator token.
	p.skipWS()
	if p.pos < len(p.input) {
		return 0, fmt.Errorf("unexpected trailing input: %q", string(p.input[p.pos:]))
	}
	return v, nil
}

func (p *exprParser) parseExpr() float64 {
	v := p.parseTerm()
	for p.pos < len(p.input) {
		switch p.input[p.pos] {
		case '+':
			p.pos++
			p.skipWS()
			v += p.parseTerm()
		case '-':
			p.pos++
			p.skipWS()
			v -= p.parseTerm()
		default:
			return v
		}
	}
	return v
}

func (p *exprParser) parseTerm() float64 {
	v := p.parsePower()
	for p.pos < len(p.input) {
		switch p.input[p.pos] {
		case '*':
			p.pos++
			p.skipWS()
			v *= p.parsePower()
		case '/':
			p.pos++
			p.skipWS()
			d := p.parsePower()
			if d == 0 {
				p.err = fmt.Errorf("division by zero")
				return 0
			}
			v /= d
		default:
			return v
		}
	}
	return v
}

func (p *exprParser) parsePower() float64 {
	base := p.parseUnary()
	if p.pos < len(p.input) && p.input[p.pos] == '^' {
		p.pos++
		p.skipWS()
		exp := p.parsePower() // right-associative
		return math.Pow(base, exp)
	}
	return base
}

func (p *exprParser) parseUnary() float64 {
	if p.pos < len(p.input) && p.input[p.pos] == '-' {
		p.pos++
		p.skipWS()
		return -p.parsePrimary()
	}
	return p.parsePrimary()
}

func (p *exprParser) parsePrimary() float64 {
	if p.pos >= len(p.input) {
		p.err = fmt.Errorf("unexpected end of expression")
		return 0
	}
	c := p.input[p.pos]
	if c == '(' {
		p.pos++
		p.skipWS()
		v := p.parseExpr()
		if p.pos < len(p.input) && p.input[p.pos] == ')' {
			p.pos++
			p.skipWS()
		} else {
			p.err = fmt.Errorf("missing closing parenthesis")
		}
		return v
	}
	if unicode.IsDigit(c) || c == '.' {
		return p.parseNumber()
	}
	// Try known functions
	if name := p.parseName(); name != "" {
		p.skipWS()
		if p.pos < len(p.input) && p.input[p.pos] == '(' {
			p.pos++
			p.skipWS()
			arg := p.parseExpr()
			if p.pos < len(p.input) && p.input[p.pos] == ')' {
				p.pos++
				p.skipWS()
			}
			switch name {
			case "sqrt":
				return math.Sqrt(arg)
			case "abs":
				return math.Abs(arg)
			case "floor":
				return math.Floor(arg)
			case "ceil":
				return math.Ceil(arg)
			case "log":
				return math.Log10(arg)
			case "ln":
				return math.Log(arg)
			}
		}
	}
	p.err = fmt.Errorf("unexpected character %q at position %d", string(c), p.pos)
	return 0
}

func (p *exprParser) parseNumber() float64 {
	start := p.pos
	for p.pos < len(p.input) && (unicode.IsDigit(p.input[p.pos]) || p.input[p.pos] == '.') {
		p.pos++
	}
	v, err := strconv.ParseFloat(string(p.input[start:p.pos]), 64)
	if err != nil {
		p.err = err
	}
	p.skipWS()
	return v
}

func (p *exprParser) parseName() string {
	if p.pos >= len(p.input) || !unicode.IsLetter(p.input[p.pos]) {
		return ""
	}
	start := p.pos
	for p.pos < len(p.input) && unicode.IsLetter(p.input[p.pos]) {
		p.pos++
	}
	return string(p.input[start:p.pos])
}

func (p *exprParser) skipWS() {
	for p.pos < len(p.input) && p.input[p.pos] == ' ' {
		p.pos++
	}
}

// ─── Exported aliases for testing ─────────────────────────────────────────
// These let test files in package agent_test access unexported types.

type ExposedCalculateTool = CalculateTool
type ExposedGetTimeTool = GetTimeTool
type ExposedSearchDocsTool = SearchDocsTool

// ─── Knowledge base ────────────────────────────────────────────────────────

type kbEntry struct {
	keywords []string
	content  string
}

var knowledgeBase = []kbEntry{
	{
		keywords: []string{"circuit breaker", "cb", "open", "closed", "half-open", "trip"},
		content: `Circuit Breaker Pattern: A circuit breaker wraps calls to a remote service and monitors for failures.
It has three states: Closed (normal operation, calls pass through), Open (calls are rejected immediately
without attempting the remote call — prevents cascading failures), and Half-Open (one probe call is allowed
to test recovery). The breaker trips to Open after a failure threshold is exceeded. After a timeout it
moves to Half-Open. Two consecutive successes close it again. The adaptive variant trips based on
error rate over a sliding window rather than consecutive failures.`,
	},
	{
		keywords: []string{"retry", "backoff", "exponential", "attempt"},
		content: `Retry with Exponential Backoff: Automatically retries failed operations with increasing delays.
The delay grows exponentially (e.g., 300ms, 600ms, 1200ms) to avoid overwhelming a recovering service.
Adding jitter (random variance) prevents thundering herd when many clients retry simultaneously.
Permanent errors should not be retried. Context cancellation and deadline propagation is essential —
never retry past the caller's deadline.`,
	},
	{
		keywords: []string{"hedge", "hedged", "parallel", "tail latency", "p99", "duplicate"},
		content: `Hedged Requests: Fire a duplicate request to the same service after a delay (e.g., 1.5s).
Return whichever response arrives first and cancel the other. This trades ~5% extra load for dramatically
reduced tail latency (p99). Best for idempotent read operations. The delay should be set to your p95
latency — that way hedging only activates for the slowest 5% of requests. Named after the financial
concept of hedging risk.`,
	},
	{
		keywords: []string{"bulkhead", "isolation", "concurrent", "semaphore", "compartment"},
		content: `Bulkhead Pattern: Limits concurrent access to a resource using a semaphore pool, isolating
different priority classes. Named after ship bulkheads that contain flooding to one compartment.
In practice: give interactive UI requests 20 slots and batch jobs 5 slots. A flood of batch requests
cannot starve interactive users. When a bulkhead is full, new requests fail immediately (ErrFull)
rather than queuing indefinitely.`,
	},
	{
		keywords: []string{"load shed", "loadshed", "overload", "aimd", "congestion", "backpressure"},
		content: `Adaptive Load Shedding: Uses the AIMD algorithm from TCP congestion control to manage
server capacity. Starts at an initial concurrency limit. When latency exceeds the threshold (e.g., 5s),
the limit is halved (multiplicative decrease). After each successful fast request, the limit increments
by 1 (additive increase). When in-flight requests exceed the limit, new arrivals are rejected with 503.
This prevents the "overload cliff" where a server accepts too much work and becomes completely unresponsive.`,
	},
	{
		keywords: []string{"semantic cache", "cache", "embedding", "cosine", "similarity", "ttl"},
		content: `Semantic Cache: Stores LLM responses indexed by embedding vectors. When a new request arrives,
its embedding is compared to cached embeddings using cosine similarity. If similarity exceeds 0.92,
the cached response is returned without calling the LLM. Unlike exact-match caches, semantically
similar questions share cached answers: "What is Go?" and "Explain the Golang language" both hit
the same cache entry. Uses nomic-embed-text (274MB) for high-quality embeddings.`,
	},
	{
		keywords: []string{"graceful degradation", "fallback", "resilience", "fault tolerance", "availability"},
		content: `Graceful Degradation: A system that never returns an error to users, instead falling back
through a tiered chain of alternatives. AgentShield's chain: primary model (best quality) → fallback
model (lower quality but fast) → semantic cache (instant, stale data) → graceful denial (always available).
Each tier has its own circuit breaker. The goal is to maintain service availability even when individual
components fail, at the cost of potentially reduced quality.`,
	},
	{
		keywords: []string{"react", "reasoning", "action", "tool", "agent", "loop"},
		content: `ReAct (Reasoning + Acting): An agent framework where the LLM alternates between Thought
(reasoning about what to do), Action (invoking a tool), and Observation (processing the tool result).
This loop repeats until the LLM produces a final Answer. Each tool call is wrapped in its own circuit
breaker — if a tool fails, the agent can reason about the failure and try alternatives. Maximum iteration
depth prevents infinite loops. Resilience at every step of the pipeline.`,
	},
}

func searchKB(query string) string {
	q := strings.ToLower(query)
	words := strings.Fields(q)

	type scored struct {
		score int
		entry kbEntry
	}
	var results []scored

	for _, entry := range knowledgeBase {
		score := 0
		for _, kw := range entry.keywords {
			if strings.Contains(q, kw) {
				score += 3
			}
		}
		for _, w := range words {
			if len(w) < 3 {
				continue
			}
			for _, kw := range entry.keywords {
				if strings.Contains(kw, w) {
					score++
				}
			}
			if strings.Contains(strings.ToLower(entry.content), w) {
				score++
			}
		}
		if score > 0 {
			results = append(results, scored{score, entry})
		}
	}

	if len(results) == 0 {
		return "No relevant documentation found for: " + query
	}

	// Sort by score descending — pick top 2
	for i := 1; i < len(results); i++ {
		for j := i; j > 0 && results[j].score > results[j-1].score; j-- {
			results[j], results[j-1] = results[j-1], results[j]
		}
	}

	var b strings.Builder
	limit := 2
	if len(results) < limit {
		limit = len(results)
	}
	for i := 0; i < limit; i++ {
		if i > 0 {
			b.WriteString("\n\n---\n\n")
		}
		b.WriteString(results[i].entry.content)
	}
	return b.String()
}
