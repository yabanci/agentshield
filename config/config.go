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
	Port           string
	AuthToken      string
	// TrustedProxies is a comma-separated CIDR list. When the TCP peer
	// falls inside any of these ranges, the ratelimit middleware honors
	// X-Forwarded-For / X-Real-IP. Empty (default) = headers ignored,
	// peer address always used. Read from AGENTSHIELD_TRUSTED_PROXIES.
	TrustedProxies string
	Logger    LoggerConfig
	Provider  ProviderConfig
	Models    ModelsConfig
	Limits    LimitsConfig
	Quality   QualityConfig
	Cache     CacheConfig
	Webhook   WebhookConfig
	Score     ScoreConfig
	MCP       MCPConfig
	OTel      OTelConfig
	ToolCache ToolCacheConfig
	ReAct     ReActConfig
}

// ToolCacheConfig controls the per-session, in-memory tool result cache that
// eliminates redundant round-trips when the ReAct loop calls the same tool
// with identical inputs within one chat turn.
type ToolCacheConfig struct {
	// Enabled toggles the cache. Default true.
	// Read from AGENTSHIELD_TOOL_CACHE_ENABLED.
	Enabled bool
	// MaxEntries caps the per-session LRU cache. Default 64.
	// Read from AGENTSHIELD_TOOL_CACHE_MAX_ENTRIES.
	MaxEntries int
}

// ReActConfig controls the ReAct loop's long-session behaviour.
type ReActConfig struct {
	// MaxTranscriptTokens is the estimated token threshold above which the
	// running Thought/Action/Observation transcript is summarized before the
	// next LLM call. Default 6000.
	// Read from AGENTSHIELD_REACT_MAX_TRANSCRIPT_TOKENS.
	MaxTranscriptTokens int
}

// OTelConfig holds OpenTelemetry exporter settings.
// All fields are read from env in env.go — no direct os.Getenv here.
type OTelConfig struct {
	// Endpoint is the OTLP gRPC target, e.g. "localhost:4317".
	// Empty means no-op tracer (OTel disabled).
	Endpoint string
	// Insecure skips TLS for the exporter. Default true for dev convenience;
	// set OTEL_EXPORTER_OTLP_INSECURE=false in production.
	Insecure bool
	// Timeout caps each export batch. Default 10s.
	Timeout time.Duration
}

// MCPConfig wires the optional 5th ReAct tool to an external MCP server.
// URL empty = MCP integration disabled (default). When set, the ReAct
// prompt grows by one tool (mcp_lookup) and per-tool CB protects it.
type MCPConfig struct {
	URL string
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
	// Kind selects the backend: "ollama" (default) or "openai" for any
	// OpenAI-compatible /v1/chat/completions endpoint (OpenAI, Groq,
	// Together, OpenRouter, vLLM, llama.cpp server, Mistral, ...).
	Kind string
	// BaseURL is the API root. For Ollama, "http://localhost:11434". For
	// OpenAI, "https://api.openai.com/v1". For Groq, "https://api.groq.com/openai/v1".
	BaseURL string
	// APIKey is the bearer token for OpenAI-compatible providers. Ignored
	// by the Ollama backend. Read from $OPENAI_API_KEY by default.
	APIKey string
	// EmbedModel is the embedding model name for OpenAI providers. Leave
	// empty to keep embeddings flowing through Ollama, which is the
	// default and keeps demo costs at zero.
	EmbedModel string
	Timeout    time.Duration
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
	// AcceptableScore is the floor below which a response counts as a
	// semantic failure. Reserved for future tuning; the quality evaluator
	// currently uses the package-level QualityAcceptable constant.
	AcceptableScore float64
	// DriftWindow / DriftSigma are reserved for tuning the long-term mean
	// drift detector. The detector currently uses fixed thresholds in
	// quality/breaker.go.
	DriftWindow int
	DriftSigma  float64
}

type CacheConfig struct {
	TTL                 time.Duration
	SimilarityThreshold float64
	MaxEntries          int
	// EmbedAsync is reserved. The cache always populates embeddings
	// asynchronously today; setting this field has no runtime effect.
	EmbedAsync bool
}

type WebhookConfig struct {
	AllowHTTP    bool
	AllowPrivate bool
	Timeout      time.Duration
}

type ScoreConfig struct {
	HistorySize int
	// LatencyP95Target is reserved for future tuning. ComputeScore in
	// telemetry/score.go currently uses fixed latency bands (<1s, <3s, ...).
	LatencyP95Target time.Duration
	// Weights are reserved for future tuning. Each component of the
	// Resilience Score is currently a fixed 20-point band. Validate()
	// still enforces the sum=100 invariant in case the field is wired later.
	Weights map[string]int
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
			MaxPromptBytes: 32 * 1024,
			ToolTimeout:    10 * time.Second,
			InteractiveSlots: 20,
			BatchSlots:       5,
			LoadshedStart:    50,
			// LoadshedWindow must sit above normal LLM latency, otherwise every
			// healthy call triggers AIMD multiplicative decrease. llama3.2 takes
			// 5–15s on a Mac; 30s leaves headroom for true overload signals.
			LoadshedWindow:      30 * time.Second,
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
		OTel: OTelConfig{
			// Insecure defaults to true: local collectors (Jaeger, Tempo,
			// HyperDX dev) almost never run TLS. Set to false in production.
			Insecure: true,
			Timeout:  10 * time.Second,
		},
		ToolCache: ToolCacheConfig{
			Enabled:    true,
			MaxEntries: 64,
		},
		ReAct: ReActConfig{
			MaxTranscriptTokens: 6000,
		},
	}
}
