// Package agent wraps Ollama LLM calls with flowguard resilience primitives.
// Degradation chain: primary model → fallback model → cache → graceful denial.
package agent

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/yabanci/flowguard/circuitbreaker"
	"github.com/yabanci/flowguard/retry"
)

const (
	ModelPrimary  = "llama3.2"
	ModelFallback = "llama3.2:1b"
)

// Tier describes which degradation level answered the request.
type Tier string

const (
	TierPrimary   Tier = "primary"
	TierFallback  Tier = "fallback"
	TierCache     Tier = "cache"
	TierDegraded  Tier = "degraded"
)

// Response is the result of a resilient LLM call.
type Response struct {
	Text   string `json:"text"`
	Tier   Tier   `json:"tier"`
	Cached bool   `json:"cached"`
}

// Status is a snapshot of the agent's current health.
type Status struct {
	PrimaryBreaker  string  `json:"primary_breaker"`
	FallbackBreaker string  `json:"fallback_breaker"`
	PrimaryKilled   bool    `json:"primary_killed"`
	CacheSize       int     `json:"cache_size"`
	TotalRequests   int64   `json:"total_requests"`
	ErrorRate       float64 `json:"error_rate"`
}

// Agent is the resilient LLM client.
type Agent struct {
	ollama          *ollamaClient
	primaryCB       *circuitbreaker.Breaker
	fallbackCB      *circuitbreaker.Breaker
	cache           *responseCache
	primaryKilled   atomic.Bool // simulated failure for demo
	fallbackKilled  atomic.Bool // simulated failure for demo
	totalRequests   atomic.Int64
}

func newAgent(ollamaURL string) *Agent {
	return &Agent{
		ollama: &ollamaClient{
			http:    newHTTPClient(),
			baseURL: ollamaURL,
		},
		primaryCB: circuitbreaker.NewAdaptive(
			20,   // window: last 20 calls
			0.5,  // trip when >50% fail
			5,    // need at least 5 samples
			circuitbreaker.WithOpenTimeout(15*time.Second),
			circuitbreaker.WithSuccessThreshold(2),
		),
		fallbackCB: circuitbreaker.New(
			circuitbreaker.WithFailureThreshold(3),
			circuitbreaker.WithOpenTimeout(30*time.Second),
		),
		cache: newResponseCache(10 * time.Minute),
	}
}

// New creates an Agent pointed at the default local Ollama instance.
func New() *Agent {
	return newAgent(ollamaBaseURL)
}

// NewWithOllamaURL creates an Agent pointed at a custom Ollama URL (for testing).
func NewWithOllamaURL(url string) *Agent {
	return newAgent(url)
}

// Ask sends a prompt through the degradation chain.
func (a *Agent) Ask(ctx context.Context, prompt string) (Response, error) {
	a.totalRequests.Add(1)

	// Tier 1: primary model with circuit breaker + retry
	if resp, ok := a.tryPrimary(ctx, prompt); ok {
		a.cache.set(prompt, resp)
		return Response{Text: resp, Tier: TierPrimary}, nil
	}

	// Tier 2: fallback model with circuit breaker
	if resp, ok := a.tryFallback(ctx, prompt); ok {
		a.cache.set(prompt, resp)
		return Response{Text: resp, Tier: TierFallback}, nil
	}

	// Tier 3: cache
	if cached, ok := a.cache.get(prompt); ok {
		return Response{Text: cached, Tier: TierCache, Cached: true}, nil
	}

	// Tier 4: graceful denial
	return Response{
		Text: "I'm currently unable to process your request. All AI tiers are unavailable. Please try again shortly.",
		Tier: TierDegraded,
	}, nil
}

func (a *Agent) tryPrimary(ctx context.Context, prompt string) (string, bool) {
	if a.primaryKilled.Load() {
		return "", false
	}

	var result string
	r := retry.New(
		retry.WithMaxRetries(3),
		retry.WithExponentialBackoff(200*time.Millisecond),
	)
	err := a.primaryCB.Do(ctx, func(ctx context.Context) error {
		return r.Do(ctx, func(ctx context.Context) error {
			resp, err := a.ollama.generate(ctx, ModelPrimary, prompt)
			if err != nil {
				return err
			}
			result = resp
			return nil
		})
	})

	if err != nil {
		return "", false
	}
	return result, true
}

func (a *Agent) tryFallback(ctx context.Context, prompt string) (string, bool) {
	if a.fallbackKilled.Load() {
		return "", false
	}
	var result string
	err := a.fallbackCB.Do(ctx, func(ctx context.Context) error {
		resp, err := a.ollama.generate(ctx, ModelFallback, prompt)
		if err != nil {
			return err
		}
		result = resp
		return nil
	})

	if err != nil {
		return "", false
	}
	return result, true
}

// KillPrimary simulates the primary model going down.
func (a *Agent) KillPrimary() {
	a.primaryKilled.Store(true)
}

// RestorePrimary simulates the primary model recovering.
func (a *Agent) RestorePrimary() {
	a.primaryKilled.Store(false)
}

// KillFallback simulates the fallback model going down.
func (a *Agent) KillFallback() {
	a.fallbackKilled.Store(true)
}

// RestoreFallback simulates the fallback model recovering.
func (a *Agent) RestoreFallback() {
	a.fallbackKilled.Store(false)
}

// Status returns a snapshot of the agent's resilience state.
func (a *Agent) Status() Status {
	primaryState := a.primaryCB.State().String()
	if a.primaryKilled.Load() {
		primaryState = "killed"
	}
	fallbackState := a.fallbackCB.State().String()
	if a.fallbackKilled.Load() {
		fallbackState = "killed"
	}
	return Status{
		PrimaryBreaker:  primaryState,
		FallbackBreaker: fallbackState,
		PrimaryKilled:   a.primaryKilled.Load(),
		CacheSize:       a.cache.size(),
		TotalRequests:   a.totalRequests.Load(),
		ErrorRate:       a.primaryCB.ErrorRate(),
	}
}

// Ping checks if Ollama is reachable.
func (a *Agent) Ping(ctx context.Context) error {
	if err := a.ollama.ping(ctx); err != nil {
		return fmt.Errorf("ollama unreachable: %w", err)
	}
	return nil
}

var ErrAllTiersDown = errors.New("all resilience tiers exhausted")
