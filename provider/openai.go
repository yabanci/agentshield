package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/yabanci/agentshield/config"
)

// OpenAIProvider talks to any OpenAI-compatible /v1/chat/completions endpoint.
// Verified compatible: OpenAI itself, Groq, Together, OpenRouter, vLLM,
// llama.cpp's server, Anyscale, Mistral. Anything that exposes the standard
// chat-completions JSON contract works without changes.
//
// Streaming uses Server-Sent Events per the OpenAI streaming spec
// (data: lines, terminating "data: [DONE]"). Embeddings call
// /v1/embeddings; if EmbedModel is empty, Embed returns ErrEmbedNotSupported
// and the caller routes through a different provider (the agent keeps
// Ollama wired as the embedder for cost reasons during demos).
type OpenAIProvider struct {
	http        *http.Client
	baseURL     string
	apiKey      string
	embedModel  string
	displayName string
}

// ErrEmbedNotSupported is returned by Embed when no embedding model is
// configured for the provider. Callers should fall through to another
// embedder (the agent wires Ollama for this) rather than treating it as a
// hard error.
var ErrEmbedNotSupported = errors.New("provider: embeddings not configured")

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIChatRequest struct {
	Model     string          `json:"model"`
	Messages  []openAIMessage `json:"messages"`
	Stream    bool            `json:"stream,omitempty"`
	MaxTokens int             `json:"max_tokens,omitempty"`
	Stop      []string        `json:"stop,omitempty"`
}

type openAIChatChoice struct {
	Message      openAIMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
	Delta        *openAIMessage `json:"delta,omitempty"`
}

type openAIChatResponse struct {
	Choices []openAIChatChoice `json:"choices"`
	Usage   struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

type openAIChatStreamChunk struct {
	Choices []struct {
		Delta openAIMessage `json:"delta"`
	} `json:"choices"`
}

type openAIEmbedRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

type openAIEmbedResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
}

// NewOpenAI builds a provider against an OpenAI-compatible endpoint.
// cfg.BaseURL must NOT include the path; standard value is
// "https://api.openai.com/v1" (or "https://api.groq.com/openai/v1" for
// Groq, etc). cfg.Timeout defaults to 60s. If cfg.APIKey is empty, calls
// will still go out — useful for unauthenticated local backends like
// llama.cpp's server.
func NewOpenAI(cfg config.ProviderConfig) *OpenAIProvider {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	return &OpenAIProvider{
		http:        &http.Client{Timeout: timeout},
		baseURL:     baseURL,
		apiKey:      cfg.APIKey,
		embedModel:  cfg.EmbedModel,
		displayName: cfg.Kind,
	}
}

// Name returns the provider's display label. Defaults to "openai" but a
// caller can pass cfg.Kind="groq" / "openrouter" / etc. to make logs and
// dashboards clearer when running against compatible services.
func (o *OpenAIProvider) Name() string {
	if o.displayName == "" {
		return "openai"
	}
	return o.displayName
}

func (o *OpenAIProvider) authHeader(req *http.Request) {
	if o.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.apiKey)
	}
	req.Header.Set("Content-Type", "application/json")
}

func (o *OpenAIProvider) Generate(ctx context.Context, req Request) (Response, error) {
	body, err := json.Marshal(openAIChatRequest{
		Model:     req.Model,
		Messages:  buildMessages(req),
		MaxTokens: req.MaxTokens,
		Stop:      req.Stop,
	})
	if err != nil {
		return Response{}, fmt.Errorf("openai marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		o.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Response{}, fmt.Errorf("openai request: %w", err)
	}
	o.authHeader(httpReq)

	resp, err := o.http.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("openai call: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		// Surface the upstream error code in logs but NOT in the response
		// to the caller — bodies can contain account/key metadata.
		_, _ = io.Copy(io.Discard, resp.Body)
		return Response{}, fmt.Errorf("openai status %d", resp.StatusCode)
	}

	var out openAIChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Response{}, fmt.Errorf("openai decode: %w", err)
	}
	if len(out.Choices) == 0 {
		return Response{}, errors.New("openai: empty choices")
	}
	return Response{
		Text:         out.Choices[0].Message.Content,
		InputTokens:  out.Usage.PromptTokens,
		OutputTokens: out.Usage.CompletionTokens,
		FinishReason: out.Choices[0].FinishReason,
	}, nil
}

// Stream consumes the OpenAI streaming format and forwards content deltas.
// The framing is:
//
//	data: {"choices":[{"delta":{"content":"hello"}}]}
//	data: [DONE]
//
// Multi-byte data: lines are joined by SSE rules (we treat each non-empty
// data: line as one JSON chunk, which matches OpenAI's actual output).
func (o *OpenAIProvider) Stream(ctx context.Context, req Request, out chan<- string) error {
	defer close(out)

	body, err := json.Marshal(openAIChatRequest{
		Model:     req.Model,
		Messages:  buildMessages(req),
		Stream:    true,
		MaxTokens: req.MaxTokens,
		Stop:      req.Stop,
	})
	if err != nil {
		return fmt.Errorf("openai stream marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		o.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("openai stream request: %w", err)
	}
	o.authHeader(httpReq)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := o.http.Do(httpReq)
	if err != nil {
		return fmt.Errorf("openai stream call: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("openai stream status %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	// Default scanner buffer (64KB) is enough for any reasonable chunk;
	// bump max so a long JSON line from a verbose model doesn't error.
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line := scanner.Bytes()
		if !bytes.HasPrefix(line, []byte("data: ")) {
			continue
		}
		payload := bytes.TrimPrefix(line, []byte("data: "))
		if bytes.Equal(payload, []byte("[DONE]")) {
			return nil
		}
		var chunk openAIChatStreamChunk
		if err := json.Unmarshal(payload, &chunk); err != nil {
			// Skip malformed chunks rather than aborting the whole stream.
			continue
		}
		for _, c := range chunk.Choices {
			if c.Delta.Content != "" {
				select {
				case out <- c.Delta.Content:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
	}
	// Scanner stopped — either clean EOF or context cancelled mid-read.
	// scanner.Err() returns nil on EOF, but if ctx fired at the same moment
	// the upstream closed the body we should surface ctx.Err() so the
	// orchestrator records a cancellation, not a clean success.
	if err := ctx.Err(); err != nil {
		return err
	}
	return scanner.Err()
}

func (o *OpenAIProvider) Embed(ctx context.Context, text string) ([]float64, error) {
	if o.embedModel == "" {
		return nil, ErrEmbedNotSupported
	}
	body, err := json.Marshal(openAIEmbedRequest{Model: o.embedModel, Input: text})
	if err != nil {
		return nil, fmt.Errorf("openai embed marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		o.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openai embed request: %w", err)
	}
	o.authHeader(httpReq)

	resp, err := o.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai embed call: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("openai embed status %d", resp.StatusCode)
	}

	var out openAIEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("openai embed decode: %w", err)
	}
	if len(out.Data) == 0 {
		return nil, errors.New("openai embed: empty data")
	}
	return out.Data[0].Embedding, nil
}

// Ping calls /v1/models which all OpenAI-compatible servers expose. Cheap
// and works without consuming inference quota, unlike a real Generate.
func (o *OpenAIProvider) Ping(ctx context.Context) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet,
		o.baseURL+"/models", nil)
	if err != nil {
		return err
	}
	if o.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)
	}
	resp, err := o.http.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("openai ping status %d", resp.StatusCode)
	}
	return nil
}

// buildMessages packs req.System + req.Prompt into the chat-completions
// messages array. We keep the conversion local so the LLMProvider
// interface stays free of provider-shaped types.
func buildMessages(req Request) []openAIMessage {
	msgs := make([]openAIMessage, 0, 2)
	if req.System != "" {
		msgs = append(msgs, openAIMessage{Role: "system", Content: req.System})
	}
	msgs = append(msgs, openAIMessage{Role: "user", Content: req.Prompt})
	return msgs
}
