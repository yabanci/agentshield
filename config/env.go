package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
)

// LoadFromEnv builds a Config from defaults, overrides with environment variables,
// then validates. Returns the first validation error if any.
func LoadFromEnv() (*Config, error) {
	c := Defaults()

	if v := os.Getenv("PORT"); v != "" {
		c.Port = v
	}
	if v := os.Getenv("OLLAMA_URL"); v != "" {
		c.Provider.BaseURL = v
	}
	// Provider selection. LLM_PROVIDER=openai switches the chat backend
	// to any OpenAI-compatible /v1/chat/completions endpoint. Embeddings
	// keep flowing through Ollama by default — set OPENAI_EMBED_MODEL to
	// route them through OpenAI too (counts against your quota).
	if v := os.Getenv("LLM_PROVIDER"); v != "" {
		c.Provider.Kind = strings.ToLower(v)
	}
	if c.Provider.Kind == "openai" {
		// Sensible default base URL for the official API. Override via
		// OPENAI_BASE_URL for Groq, OpenRouter, vLLM, etc.
		if c.Provider.BaseURL == "" || c.Provider.BaseURL == "http://localhost:11434" {
			c.Provider.BaseURL = "https://api.openai.com/v1"
		}
		// OpenAI's flagship pair is gpt-4o (primary) + gpt-4o-mini (fallback);
		// these defaults can be overridden via OPENAI_PRIMARY_MODEL /
		// OPENAI_FALLBACK_MODEL.
		if c.Models.Primary == "llama3.2" {
			c.Models.Primary = "gpt-4o-mini"
		}
		if c.Models.Fallback == "llama3.2:1b" {
			c.Models.Fallback = "gpt-4o-mini"
		}
	}
	if v := os.Getenv("OPENAI_BASE_URL"); v != "" {
		c.Provider.BaseURL = v
	}
	if v := os.Getenv("OPENAI_API_KEY"); v != "" {
		c.Provider.APIKey = v
	}
	if v := os.Getenv("OPENAI_EMBED_MODEL"); v != "" {
		c.Provider.EmbedModel = v
	}
	if v := os.Getenv("OPENAI_PRIMARY_MODEL"); v != "" {
		c.Models.Primary = v
	}
	if v := os.Getenv("OPENAI_FALLBACK_MODEL"); v != "" {
		c.Models.Fallback = v
	}
	if v := os.Getenv("AGENTSHIELD_AUTH_TOKEN"); v != "" {
		c.AuthToken = v
	}
	if v := os.Getenv("AGENTSHIELD_TRUSTED_PROXIES"); v != "" {
		c.TrustedProxies = v
	}
	// MCP integration. Optional 5th ReAct tool (mcp_lookup) wires through
	// the existing per-tool CB. Unset = MCP disabled, default ReAct tool
	// set unchanged. For the demo we ship cmd/mcp-mock/ which exposes
	// /mcp/call + /mcp/kill + /mcp/restore on :8081.
	if v := os.Getenv("MCP_URL"); v != "" {
		c.MCP.URL = v
	}
	if os.Getenv("AGENTSHIELD_ALLOW_HTTP_WEBHOOK") == "true" {
		c.Webhook.AllowHTTP = true
	}
	if os.Getenv("AGENTSHIELD_ALLOW_PRIVATE_WEBHOOK") == "true" {
		c.Webhook.AllowPrivate = true
	}
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		switch strings.ToLower(v) {
		case "debug":
			c.Logger.Level = slog.LevelDebug
		case "info":
			c.Logger.Level = slog.LevelInfo
		case "warn":
			c.Logger.Level = slog.LevelWarn
		case "error":
			c.Logger.Level = slog.LevelError
		}
	}
	if v := os.Getenv("LOG_FORMAT"); v == "json" || v == "text" {
		c.Logger.Format = v
	}

	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	return c, nil
}
