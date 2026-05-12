package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const EmbedModel = "nomic-embed-text"

type ollamaClient struct {
	http    *http.Client
	baseURL string
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

func newHTTPClient() *http.Client {
	return &http.Client{Timeout: 60 * time.Second}
}

func (c *ollamaClient) generate(ctx context.Context, model, prompt string) (string, error) {
	body, err := json.Marshal(ollamaRequest{Model: model, Prompt: prompt, Stream: false})
	if err != nil {
		return "", fmt.Errorf("ollama marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("ollama request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama call: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama status %d", resp.StatusCode)
	}

	var out ollamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("ollama decode: %w", err)
	}
	return out.Response, nil
}

// Stream calls Ollama with stream=true and sends each token to the tokens channel.
// Closes the channel when done or on error.
func (c *ollamaClient) stream(ctx context.Context, model, prompt string, tokens chan<- string) error {
	body, err := json.Marshal(ollamaRequest{Model: model, Prompt: prompt, Stream: true})
	if err != nil {
		return fmt.Errorf("ollama stream marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("ollama stream request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Use a long-lived client for streaming
	streamClient := &http.Client{Timeout: 120 * time.Second}
	resp, err := streamClient.Do(req)
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
			tokens <- chunk.Response
		}
		if chunk.Done {
			return nil
		}
	}
	return scanner.Err()
}

// Embed generates a vector embedding for the given text using nomic-embed-text.
func (c *ollamaClient) embed(ctx context.Context, text string) ([]float64, error) {
	body, err := json.Marshal(ollamaEmbedRequest{Model: EmbedModel, Prompt: text})
	if err != nil {
		return nil, fmt.Errorf("embed marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("embed request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
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

func (c *ollamaClient) ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/tags", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama ping status %d", resp.StatusCode)
	}
	return nil
}
