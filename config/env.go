package config

import (
	"fmt"
	"os"
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
	if v := os.Getenv("AGENTSHIELD_AUTH_TOKEN"); v != "" {
		c.AuthToken = v
	}
	if os.Getenv("AGENTSHIELD_ALLOW_HTTP_WEBHOOK") == "true" {
		c.Webhook.AllowHTTP = true
	}
	if os.Getenv("AGENTSHIELD_ALLOW_PRIVATE_WEBHOOK") == "true" {
		c.Webhook.AllowPrivate = true
	}

	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	return c, nil
}
