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
}

type LoggerConfig struct {
	Level  slog.Level
	Format string // "text" | "json"
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
	}
}

// Suppress unused-import warning until later tasks reference time.Duration fields.
var _ = time.Second
