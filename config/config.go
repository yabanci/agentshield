// Package config holds AgentShield's typed runtime configuration.
// Every os.Getenv read in the project lives here; no other package may import "os"
// for environment access.
package config

import (
	"log/slog"
	"time"
)

type Config struct {
	Port      string
	AuthToken string
	Logger    LoggerConfig
	Provider  ProviderConfig
	Models    ModelsConfig
}

type LoggerConfig struct {
	Level  slog.Level
	Format string // "text" | "json"
}

type ProviderConfig struct {
	Kind    string
	BaseURL string
	Timeout time.Duration
}

type ModelsConfig struct {
	Primary   string
	Fallback  string
	Embedding string
}

// Defaults returns a Config populated with safe production defaults.
// Validate is NOT called here — caller must invoke Validate after merging env.
func Defaults() *Config {
	return &Config{
		Port: "8080",
		Logger: LoggerConfig{
			Level:  slog.LevelInfo,
			Format: "text",
		},
		Provider: ProviderConfig{
			Kind:    "ollama",
			BaseURL: "http://localhost:11434",
			Timeout: 60 * time.Second,
		},
		Models: ModelsConfig{
			Primary:   "llama3.2",
			Fallback:  "llama3.2:1b",
			Embedding: "nomic-embed-text",
		},
	}
}
