// Package naive implements the "naive integration" path used as the baseline
// in the AgentShield benchmark harness.
//
// # What this represents
//
// This mirrors what a minimal LangChain LLMChain (or any direct LLM client)
// does for the three unhappy paths AgentShield targets:
//
//   - No quality check: the client returns whatever the model sent — it has no
//     concept of repetition, refusal markers, or semantic coherence.
//   - Single transport retry: one retry on explicit transport error (5xx,
//     timeout) with no backoff smarts. LangChain's default is one retry;
//     production teams often leave this at zero.
//   - No semantic cache: if both primary and retry fail, the call errors.
//   - No graceful denial: a failed call propagates as an error to the caller,
//     which typically means a 500 to the end user.
//
// The 30-second timeout is deliberately generous — real "naive" integrations
// often use the HTTP client default (no timeout) or whatever the framework
// hard-codes. 30s is a better benchmark comparison point because it is the
// value TrueFoundry's brief explicitly calls out.
//
// # What this does NOT represent
//
// We are benchmarking a *pattern* (naive direct call), not the LangChain
// framework itself. LangChain has many optional resilience primitives
// (callbacks, fallback_llms, cache). This client deliberately omits them
// to demonstrate the gap that exists when an LLM integration ships without
// deliberately enabling those primitives — which, in practice, is most of them.
package naive

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	// DefaultTimeout is the per-request timeout mirroring the TrueFoundry brief.
	DefaultTimeout = 30 * time.Second
	// defaultMaxRetries is the number of times to retry a transport error.
	// Mirrors LangChain's default max_retries=1.
	defaultMaxRetries = 1
)

// generateRequest is the minimal JSON body for an Ollama /api/generate call.
type generateRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

// generateResponse is the subset of the Ollama response envelope we read.
type generateResponse struct {
	Response string `json:"response"`
}

// Client is a bare-minimum LLM HTTP client.
// One timeout, one retry, no quality check, no cache, no graceful denial.
type Client struct {
	baseURL    string
	model      string
	httpClient *http.Client
	maxRetries int
	scenario   string // X-Bench-Scenario header value passed to fake backend
}

// Option configures a Client.
type Option func(*Client)

// WithScenario injects the X-Bench-Scenario header so the fake backend serves
// the right failure mode. Do not set this in production code — it is a
// bench-harness hook only.
func WithScenario(s string) Option {
	return func(c *Client) { c.scenario = s }
}

// WithMaxRetries overrides the default retry count.
func WithMaxRetries(n int) Option {
	return func(c *Client) { c.maxRetries = n }
}

// New creates a Client pointed at baseURL serving model.
func New(baseURL, model string, opts ...Option) *Client {
	c := &Client{
		baseURL:    baseURL,
		model:      model,
		httpClient: &http.Client{Timeout: DefaultTimeout},
		maxRetries: defaultMaxRetries,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Generate calls the LLM backend and returns the raw response text.
// It retries on transport errors up to maxRetries times, then gives up.
// No quality check is performed — whatever the model said is returned.
func (c *Client) Generate(ctx context.Context, prompt string) (string, error) {
	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		text, err := c.doGenerate(ctx, prompt)
		if err == nil {
			return text, nil
		}
		lastErr = err
		// Naive: retry every error indiscriminately. No backoff.
		// (A slightly-less-naive integration would at least not retry on 4xx,
		// but the vast majority of LLM integrations in the wild don't bother.)
	}
	return "", fmt.Errorf("naive: all %d attempts failed: %w", c.maxRetries+1, lastErr)
}

func (c *Client) doGenerate(ctx context.Context, prompt string) (string, error) {
	body := generateRequest{Model: c.model, Prompt: prompt, Stream: false}
	b, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("naive: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/api/generate", strings.NewReader(string(b)))
	if err != nil {
		return "", fmt.Errorf("naive: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.scenario != "" {
		req.Header.Set("X-Bench-Scenario", c.scenario)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("naive: http: %w", err)
	}

	if resp.StatusCode >= 500 {
		_ = resp.Body.Close()
		// Treat 5xx as transport error — retry.
		return "", fmt.Errorf("naive: upstream %d", resp.StatusCode)
	}

	raw, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return "", fmt.Errorf("naive: read body: %w", err)
	}

	var result generateResponse
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("naive: decode: %w", err)
	}
	return result.Response, nil
}
