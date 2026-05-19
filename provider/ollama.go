package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/yabanci/agentshield/config"
)

// defaultOllamaEmbedModel is used when ProviderConfig.EmbedModel is unset.
// nomic-embed-text ships in the Quick Start so this default keeps existing
// deployments working without configuration.
const defaultOllamaEmbedModel = "nomic-embed-text"

type OllamaProvider struct {
	http       *http.Client
	streamHTTP *http.Client // longer timeout for streaming generations
	baseURL    string
	embedModel string
}

type ollamaRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

type ollamaResponse struct {
	Response string `json:"response"`
	Done     bool   `json:"done"`
}

type ollamaEmbedRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type ollamaEmbedResponse struct {
	Embedding []float64 `json:"embedding"`
}

// NewOllama constructs a provider talking to an Ollama backend at cfg.BaseURL.
// Timeout defaults to 60s if cfg.Timeout is zero. EmbedModel defaults to
// "nomic-embed-text" if not set on cfg — overridable so an operator can
// run a different embedding model without modifying source.
func NewOllama(cfg config.ProviderConfig) *OllamaProvider {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	embedModel := cfg.EmbedModel
	if embedModel == "" {
		embedModel = defaultOllamaEmbedModel
	}
	// Custom transport with a short DIAL timeout: when Ollama is down,
	// each tier's HTTP call would otherwise wait for the OS-level connect
	// timeout (typically ~75s on macOS, ~21s on Linux) before falling
	// through to the next tier. That makes graceful denial take 1.8+
	// seconds — round-11 smoke test caught it. 300 ms is well above any
	// realistic local-network latency and lets the orchestrator move
	// through all four tiers in under a second.
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   300 * time.Millisecond,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:        20,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 5 * time.Second,
	}
	// otelhttp.NewTransport wraps the custom transport as the inner
	// RoundTripper so the dial timeout is preserved while adding OTel
	// client-span creation and W3C traceparent header injection.
	traced := otelhttp.NewTransport(transport)
	return &OllamaProvider{
		http: &http.Client{Transport: traced, Timeout: timeout},
		// streamHTTP is a separate client because streaming has a much longer
		// upper bound than a non-streaming Generate. Reusing across calls
		// keeps connections in the pool and avoids the fd leak from
		// constructing a fresh client on every Stream invocation. Shares
		// the dial timeout — only the overall request budget differs.
		streamHTTP: &http.Client{Transport: traced, Timeout: 120 * time.Second},
		baseURL:    cfg.BaseURL,
		embedModel: embedModel,
	}
}

func (o *OllamaProvider) Name() string { return "ollama" }

func (o *OllamaProvider) Generate(ctx context.Context, req Request) (Response, error) {
	body, err := json.Marshal(ollamaRequest{Model: req.Model, Prompt: req.Prompt, Stream: false})
	if err != nil {
		return Response{}, fmt.Errorf("ollama marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return Response{}, fmt.Errorf("ollama request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := o.http.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("ollama call: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return Response{}, fmt.Errorf("ollama status %d", resp.StatusCode)
	}

	var out ollamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Response{}, fmt.Errorf("ollama decode: %w", err)
	}
	return Response{Text: out.Response, FinishReason: "stop"}, nil
}

// Stream calls Ollama with stream=true. Closes `out` on completion or error
// (per the LLMProvider contract).
func (o *OllamaProvider) Stream(ctx context.Context, req Request, out chan<- string) error {
	defer close(out)

	body, err := json.Marshal(ollamaRequest{Model: req.Model, Prompt: req.Prompt, Stream: true})
	if err != nil {
		return fmt.Errorf("ollama stream marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("ollama stream request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := o.streamHTTP.Do(httpReq)
	if err != nil {
		return fmt.Errorf("ollama stream call: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama stream status %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		var chunk ollamaResponse
		if err := json.Unmarshal(scanner.Bytes(), &chunk); err != nil {
			continue
		}
		if chunk.Response != "" {
			out <- chunk.Response
		}
		if chunk.Done {
			return nil
		}
	}
	return scanner.Err()
}

func (o *OllamaProvider) Embed(ctx context.Context, text string) ([]float64, error) {
	body, err := json.Marshal(ollamaEmbedRequest{Model: o.embedModel, Prompt: text})
	if err != nil {
		return nil, fmt.Errorf("embed marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/api/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("embed request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := o.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("embed call: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embed status %d", resp.StatusCode)
	}

	var out ollamaEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("embed decode: %w", err)
	}
	return out.Embedding, nil
}

func (o *OllamaProvider) Ping(ctx context.Context) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, o.baseURL+"/api/tags", nil)
	if err != nil {
		return err
	}
	resp, err := o.http.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama ping status %d", resp.StatusCode)
	}
	return nil
}
