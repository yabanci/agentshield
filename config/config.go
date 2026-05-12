// Package config holds AgentShield's typed runtime configuration.
// Every os.Getenv read in the project lives here; no other package may import "os"
// for environment access.
package config

import (
	"fmt"
	"io"
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

// NewLogger builds a *slog.Logger from cfg.Logger. The format selects between
// TextHandler (human-friendly, default) and JSONHandler (production / log
// aggregators). Pass os.Stdout in normal use.
func (c *Config) NewLogger(out io.Writer) *slog.Logger {
	opts := &slog.HandlerOptions{Level: c.Logger.Level}
	var h slog.Handler
	switch c.Logger.Format {
	case "json":
		h = slog.NewJSONHandler(out, opts)
	default:
		h = slog.NewTextHandler(out, opts)
	}
	return slog.New(h)
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

// Validate fails fast on misconfigurations that would only surface at request time.
func (c *Config) Validate() error {
	if c.Limits.InteractiveSlots <= 0 {
		return fmt.Errorf("Limits.InteractiveSlots must be > 0, got %d", c.Limits.InteractiveSlots)
	}
	if c.Limits.BatchSlots <= 0 {
		return fmt.Errorf("Limits.BatchSlots must be > 0, got %d", c.Limits.BatchSlots)
	}
	if c.Limits.LoadshedStart <= 0 {
		return fmt.Errorf("Limits.LoadshedStart must be > 0, got %d", c.Limits.LoadshedStart)
	}
	if c.Limits.PrimaryCBErrorRate <= 0 || c.Limits.PrimaryCBErrorRate >= 1 {
		return fmt.Errorf("Limits.PrimaryCBErrorRate must be in (0,1), got %v", c.Limits.PrimaryCBErrorRate)
	}
	if c.Score.LatencyP95Target <= 0 {
		return fmt.Errorf("Score.LatencyP95Target must be > 0, got %v", c.Score.LatencyP95Target)
	}
	sum := 0
	for _, w := range c.Score.Weights {
		sum += w
	}
	if sum != 100 {
		return fmt.Errorf("Score.Weights must sum to 100, got %d", sum)
	}
	return nil
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
