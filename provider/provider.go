// Package provider abstracts LLM backends behind a single interface.
// All references to specific clients (Ollama, OpenAI, Groq, ...) live in this package.
// Consumers depend on LLMProvider, never on concrete types.
package provider

import "context"

// Request is a provider-agnostic LLM request.
type Request struct {
	Model     string
	Prompt    string
	System    string   // optional
	MaxTokens int      // 0 = provider default
	Stop      []string // optional
}

// Response is a provider-agnostic LLM response.
type Response struct {
	Text         string
	InputTokens  int
	OutputTokens int
	FinishReason string // "stop", "length", ...
}

// LLMProvider is the contract every backend must satisfy.
//
// Channel ownership for Stream:
//   - The caller PROVIDES the out channel.
//   - The provider OWNS sending and is REQUIRED to close `out` when the stream ends
//     (success or error). Callers must NOT close `out` themselves.
//   - This convention prevents double-close panics in code that fan-ins multiple streams.
type LLMProvider interface {
	Generate(ctx context.Context, req Request) (Response, error)
	Stream(ctx context.Context, req Request, out chan<- string) error
	Embed(ctx context.Context, text string) ([]float64, error)
	Ping(ctx context.Context) error
	Name() string
}

// Embedder is the narrow subset of LLMProvider that the quality evaluator and
// semantic cache depend on. Use this in those packages to enforce ISP.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float64, error)
}
