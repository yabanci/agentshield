// Package runner drives both the naive and AgentShield paths against the fake
// backend for a given failure scenario, collecting per-request timing and
// quality metrics, then aggregating them into a ScenarioResult.
package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/yabanci/agentshield/bench/naive"
	"github.com/yabanci/agentshield/quality"
)

// Scenario names match the X-Bench-Scenario header values.
const (
	ScenarioGarbage  = "garbage"
	ScenarioBrownout = "brownout"
	ScenarioDown     = "down"
)

// Sample is one request's result on one path.
type Sample struct {
	LatencyMS int64   // wall-clock duration of the call
	Success   bool    // response returned within the timeout (naive's bar)
	Useful    bool    // response was non-garbage (quality score >= threshold)
	Score     float64 // raw quality score, 0.0–1.0
	Tier      string  // "naive" | "primary" | "fallback" | "cache" | "degraded"
}

// Stats summarises a slice of Samples.
type Stats struct {
	P50MS    int64
	P95MS    int64
	P99MS    int64
	StdDevMS float64
	// SuccessRate is the fraction of calls that returned a response (not error/timeout).
	SuccessRate float64
	// UsefulRate is the fraction of calls that returned a quality-passing response.
	UsefulRate float64
	// TimeToFirstUsefulMS is the latency of the first successful+useful sample, -1 if none.
	TimeToFirstUsefulMS int64
}

// PathResult holds raw samples and derived stats for one path.
type PathResult struct {
	Path    string // "naive" | "agentshield"
	Samples []Sample
	Stats   Stats
}

// ScenarioResult bundles both paths for one failure scenario.
type ScenarioResult struct {
	Scenario string
	Naive    PathResult
	Shield   PathResult
}

// AgentShieldClient is a minimal client that calls the AgentShield orchestrator
// via the in-process Ollama-compatible wrapper.  Rather than spinning up a full
// agent.Agent (which requires config wiring, telemetry, etc.), the runner talks
// directly to the fake backend through its own tiered-defense logic.
//
// The "AgentShield" path in the bench is:
//  1. Primary call to fake backend (via naive HTTP client but with quality gate)
//  2. Fallback call to a second fake backend endpoint (same server, different path)
//  3. Semantic cache lookup (in-memory, warm after N good responses)
//  4. Graceful denial text
//
// This is a bench-only minimal reimplementation, not the full orchestrator.
// It intentionally uses only public APIs from quality/ — no duplication.
type AgentShieldClient struct {
	baseURL   string
	scenario  string
	eval  *quality.QualityEvaluator
	cache map[string]string // simple prompt→response in-memory cache
}

// NewAgentShieldClient constructs the AgentShield-path client.
func NewAgentShieldClient(baseURL, scenario string) *AgentShieldClient {
	return &AgentShieldClient{
		baseURL:  baseURL,
		scenario: scenario,
		eval:     quality.NewEvaluator(nil),
		cache:    make(map[string]string),
	}
}

// Generate runs the 4-tier chain and returns (text, tier, error).
func (c *AgentShieldClient) Generate(ctx context.Context, prompt string) (string, string, error) {
	// Tier 1: primary — call backend, check quality.
	text, err := c.rawCall(ctx, prompt)
	if err == nil {
		qr := c.eval.Evaluate(ctx, prompt, text)
		if qr.Score >= qualityThreshold {
			// Good response: warm the cache for future requests.
			c.cache[simpleCacheKey(prompt)] = text
			return text, "primary", nil
		}
		// Quality failure → fall through to next tier.
	}

	// Tier 2: fallback — same backend, but using a no-scenario call that
	// returns a good response. In the real orchestrator this is a different
	// provider; here we simulate it by requesting with no scenario header.
	fallbackText, fallbackErr := c.rawCallNoScenario(ctx, prompt)
	if fallbackErr == nil {
		qr := c.eval.Evaluate(ctx, prompt, fallbackText)
		if qr.Score >= qualityThreshold {
			c.cache[simpleCacheKey(prompt)] = fallbackText
			return fallbackText, "fallback", nil
		}
	}

	// Tier 3: semantic cache.
	if cached, ok := c.cache[simpleCacheKey(prompt)]; ok {
		return cached, "cache", nil
	}

	// Tier 4: graceful denial.
	return "All AI tiers are currently unavailable. Please try again shortly.", "degraded", nil
}

// rawCall calls the fake backend with the bench scenario header.
func (c *AgentShieldClient) rawCall(ctx context.Context, prompt string) (string, error) {
	return callBackend(ctx, c.baseURL, prompt, c.scenario)
}

// rawCallNoScenario calls the fake backend without a scenario header,
// which always returns a good response (simulates the fallback provider).
func (c *AgentShieldClient) rawCallNoScenario(ctx context.Context, prompt string) (string, error) {
	return callBackend(ctx, c.baseURL, prompt, "")
}

// callBackend makes one HTTP call to the fake backend.
func callBackend(ctx context.Context, baseURL, prompt, scenario string) (string, error) {
	type reqBody struct {
		Model  string `json:"model"`
		Prompt string `json:"prompt"`
		Stream bool   `json:"stream"`
	}
	b, _ := json.Marshal(reqBody{Model: "bench-model", Prompt: prompt, Stream: false})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		baseURL+"/api/generate", strings.NewReader(string(b)))
	if err != nil {
		return "", fmt.Errorf("runner: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if scenario != "" {
		req.Header.Set("X-Bench-Scenario", scenario)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("runner: http: %w", err)
	}
	if resp.StatusCode >= 500 {
		_ = resp.Body.Close()
		return "", fmt.Errorf("runner: upstream %d", resp.StatusCode)
	}

	raw, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return "", fmt.Errorf("runner: read: %w", err)
	}
	var result struct {
		Response string `json:"response"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("runner: decode: %w", err)
	}
	return result.Response, nil
}

// BackgroundCtx returns a plain background context for use by callers that
// need a context outside of a per-request timeout (e.g., warmup calls).
func BackgroundCtx() context.Context {
	return context.Background()
}

// simpleCacheKey returns a simple key for the in-memory cache.
// The real orchestrator uses a semantic embedder; here we use exact-match
// to keep the bench self-contained. The bench uses a fixed prompt set so
// exact-match cache is warmed on the first successful call.
func simpleCacheKey(prompt string) string {
	if len(prompt) > 64 {
		return prompt[:64]
	}
	return prompt
}

// RunScenario drives n requests through both paths for the given scenario.
// baseURL is the fake backend URL; prompts are the fixed prompts to cycle through.
func RunScenario(scenario, baseURL string, n int, prompts []string) ScenarioResult {
	result := ScenarioResult{Scenario: scenario}

	naiveClient := naive.New(baseURL, "bench-model",
		naive.WithScenario(scenario),
		naive.WithMaxRetries(1),
	)
	shieldClient := NewAgentShieldClient(baseURL, scenario)

	result.Naive = runPath("naive", n, prompts, func(ctx context.Context, prompt string) (string, string, error) {
		text, err := naiveClient.Generate(ctx, prompt)
		return text, "naive", err
	})

	result.Shield = runPath("agentshield", n, prompts, func(ctx context.Context, prompt string) (string, string, error) {
		return shieldClient.Generate(ctx, prompt)
	})

	return result
}

// generateFn is the function signature for a single LLM call.
// Returns (text, tier, error).
type generateFn func(ctx context.Context, prompt string) (string, string, error)

func runPath(pathName string, n int, prompts []string, fn generateFn) PathResult {
	samples := make([]Sample, 0, n)
	firstUsefulMS := int64(-1)
	prompt := prompts[0]

	for i := 0; i < n; i++ {
		if len(prompts) > 0 {
			prompt = prompts[i%len(prompts)]
		}

		ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
		start := time.Now()
		text, tier, err := fn(ctx, prompt)
		latency := time.Since(start).Milliseconds()
		cancel()

		s := Sample{
			LatencyMS: latency,
			Success:   err == nil && text != "",
			Tier:      tier,
		}
		if s.Success {
			s.Score = score(context.Background(), prompt, text)
			s.Useful = isUseful(context.Background(), prompt, text)
		}
		if s.Useful && firstUsefulMS < 0 {
			firstUsefulMS = latency
		}
		samples = append(samples, s)
	}

	pr := PathResult{Path: pathName, Samples: samples}
	pr.Stats = computeStats(samples, firstUsefulMS)
	return pr
}

func computeStats(samples []Sample, firstUsefulMS int64) Stats {
	if len(samples) == 0 {
		return Stats{TimeToFirstUsefulMS: -1}
	}

	latencies := make([]int64, len(samples))
	successCount := 0
	usefulCount := 0
	for i, s := range samples {
		latencies[i] = s.LatencyMS
		if s.Success {
			successCount++
		}
		if s.Useful {
			usefulCount++
		}
	}

	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	n := len(latencies)

	mean := 0.0
	for _, l := range latencies {
		mean += float64(l)
	}
	mean /= float64(n)

	variance := 0.0
	for _, l := range latencies {
		d := float64(l) - mean
		variance += d * d
	}
	variance /= float64(n)

	return Stats{
		P50MS:               latencies[p(n, 50)],
		P95MS:               latencies[p(n, 95)],
		P99MS:               latencies[p(n, 99)],
		StdDevMS:            math.Sqrt(variance),
		SuccessRate:         float64(successCount) / float64(n),
		UsefulRate:          float64(usefulCount) / float64(n),
		TimeToFirstUsefulMS: firstUsefulMS,
	}
}

// p returns the index for percentile pct in a sorted slice of length n.
func p(n, pct int) int {
	idx := int(math.Ceil(float64(pct)/100.0*float64(n))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= n {
		idx = n - 1
	}
	return idx
}
