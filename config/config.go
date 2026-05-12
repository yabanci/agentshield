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
	Limits    LimitsConfig
	Quality   QualityConfig
	Cache     CacheConfig
	Webhook   WebhookConfig
	Score     ScoreConfig
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

type LimitsConfig struct {
	MaxPromptBytes      int
	ToolTimeout         time.Duration
	InteractiveSlots    int
	BatchSlots          int
	LoadshedStart       int
	LoadshedWindow      time.Duration
	PrimaryCBWindow     int
	PrimaryCBErrorRate  float64
	FallbackCBThreshold int
	HedgeDelay          time.Duration
	RetryMax            int
	RetryBaseBackoff    time.Duration
}

type QualityConfig struct {
	AcceptableScore float64
	DriftWindow     int
	DriftSigma      float64
}

type CacheConfig struct {
	TTL                 time.Duration
	SimilarityThreshold float64
	MaxEntries          int
	EmbedAsync          bool
}

type WebhookConfig struct {
	AllowHTTP    bool
	AllowPrivate bool
	Timeout      time.Duration
}

type ScoreConfig struct {
	HistorySize      int
	LatencyP95Target time.Duration
	Weights          map[string]int
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
		Limits: LimitsConfig{
			MaxPromptBytes:      32 * 1024,
			ToolTimeout:         10 * time.Second,
			InteractiveSlots:    20,
			BatchSlots:          5,
			LoadshedStart:       50,
			LoadshedWindow:      5 * time.Second,
			PrimaryCBWindow:     20,
			PrimaryCBErrorRate:  0.5,
			FallbackCBThreshold: 3,
			HedgeDelay:          1500 * time.Millisecond,
			RetryMax:            2,
			RetryBaseBackoff:    300 * time.Millisecond,
		},
		Quality: QualityConfig{
			AcceptableScore: 0.45,
			DriftWindow:     50,
			DriftSigma:      2.0,
		},
		Cache: CacheConfig{
			TTL:                 10 * time.Minute,
			SimilarityThreshold: 0.92,
			MaxEntries:          1024,
			EmbedAsync:          true,
		},
		Webhook: WebhookConfig{
			Timeout: 5 * time.Second,
		},
		Score: ScoreConfig{
			HistorySize:      60,
			LatencyP95Target: 3 * time.Second,
			Weights: map[string]int{
				"transport":    20,
				"quality":      20,
				"cache":        20,
				"availability": 20,
				"latency":      20,
			},
		},
	}
}
