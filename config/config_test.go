package config

import (
	"log/slog"
	"testing"
	"time"
)

func TestDefaults_Logger(t *testing.T) {
	c := Defaults()
	if c.Logger.Level != slog.LevelInfo {
		t.Errorf("Logger.Level = %v, want INFO", c.Logger.Level)
	}
	if c.Logger.Format != "text" {
		t.Errorf("Logger.Format = %q, want text", c.Logger.Format)
	}
}

func TestDefaults_ProviderAndModels(t *testing.T) {
	c := Defaults()
	if c.Provider.Kind != "ollama" {
		t.Errorf("Provider.Kind = %q, want ollama", c.Provider.Kind)
	}
	if c.Provider.BaseURL != "http://localhost:11434" {
		t.Errorf("Provider.BaseURL = %q, want http://localhost:11434", c.Provider.BaseURL)
	}
	if c.Provider.Timeout != 60*time.Second {
		t.Errorf("Provider.Timeout = %v, want 60s", c.Provider.Timeout)
	}
	if c.Models.Primary != "llama3.2" {
		t.Errorf("Models.Primary = %q, want llama3.2", c.Models.Primary)
	}
	if c.Models.Fallback != "llama3.2:1b" {
		t.Errorf("Models.Fallback = %q, want llama3.2:1b", c.Models.Fallback)
	}
	if c.Models.Embedding != "nomic-embed-text" {
		t.Errorf("Models.Embedding = %q, want nomic-embed-text", c.Models.Embedding)
	}
}

func TestDefaults_Limits(t *testing.T) {
	c := Defaults()
	cases := []struct {
		name string
		got  any
		want any
	}{
		{"MaxPromptBytes", c.Limits.MaxPromptBytes, 32 * 1024},
		{"ToolTimeout", c.Limits.ToolTimeout, 10 * time.Second},
		{"InteractiveSlots", c.Limits.InteractiveSlots, 20},
		{"BatchSlots", c.Limits.BatchSlots, 5},
		{"LoadshedStart", c.Limits.LoadshedStart, 50},
		{"LoadshedWindow", c.Limits.LoadshedWindow, 5 * time.Second},
		{"PrimaryCBWindow", c.Limits.PrimaryCBWindow, 20},
		{"PrimaryCBErrorRate", c.Limits.PrimaryCBErrorRate, 0.5},
		{"FallbackCBThreshold", c.Limits.FallbackCBThreshold, 3},
		{"HedgeDelay", c.Limits.HedgeDelay, 1500 * time.Millisecond},
		{"RetryMax", c.Limits.RetryMax, 2},
		{"RetryBaseBackoff", c.Limits.RetryBaseBackoff, 300 * time.Millisecond},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %v, want %v", tc.name, tc.got, tc.want)
		}
	}
}

func TestDefaults_QualityCacheWebhookScore(t *testing.T) {
	c := Defaults()
	if c.Quality.AcceptableScore != 0.45 {
		t.Errorf("Quality.AcceptableScore = %v, want 0.45", c.Quality.AcceptableScore)
	}
	if c.Quality.DriftWindow != 50 {
		t.Errorf("Quality.DriftWindow = %d, want 50", c.Quality.DriftWindow)
	}
	if c.Quality.DriftSigma != 2.0 {
		t.Errorf("Quality.DriftSigma = %v, want 2.0", c.Quality.DriftSigma)
	}
	if c.Cache.TTL != 10*time.Minute {
		t.Errorf("Cache.TTL = %v, want 10m", c.Cache.TTL)
	}
	if c.Cache.SimilarityThreshold != 0.92 {
		t.Errorf("Cache.SimilarityThreshold = %v, want 0.92", c.Cache.SimilarityThreshold)
	}
	if c.Cache.MaxEntries != 1024 {
		t.Errorf("Cache.MaxEntries = %d, want 1024", c.Cache.MaxEntries)
	}
	if !c.Cache.EmbedAsync {
		t.Errorf("Cache.EmbedAsync = false, want true")
	}
	if c.Webhook.Timeout != 5*time.Second {
		t.Errorf("Webhook.Timeout = %v, want 5s", c.Webhook.Timeout)
	}
	if c.Webhook.AllowHTTP {
		t.Errorf("Webhook.AllowHTTP = true, want false")
	}
	if c.Webhook.AllowPrivate {
		t.Errorf("Webhook.AllowPrivate = true, want false")
	}
	if c.Score.HistorySize != 60 {
		t.Errorf("Score.HistorySize = %d, want 60", c.Score.HistorySize)
	}
	if c.Score.LatencyP95Target != 3*time.Second {
		t.Errorf("Score.LatencyP95Target = %v, want 3s", c.Score.LatencyP95Target)
	}
	wantWeights := map[string]int{"transport": 20, "quality": 20, "cache": 20, "availability": 20, "latency": 20}
	for k, v := range wantWeights {
		if c.Score.Weights[k] != v {
			t.Errorf("Score.Weights[%q] = %d, want %d", k, c.Score.Weights[k], v)
		}
	}
}

func TestLoadFromEnv_Overrides(t *testing.T) {
	t.Setenv("PORT", "9090")
	t.Setenv("OLLAMA_URL", "http://ollama.example:11434")
	t.Setenv("AGENTSHIELD_AUTH_TOKEN", "secret-xyz")
	t.Setenv("AGENTSHIELD_ALLOW_HTTP_WEBHOOK", "true")
	t.Setenv("AGENTSHIELD_ALLOW_PRIVATE_WEBHOOK", "true")

	c, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv: %v", err)
	}
	if c.Port != "9090" {
		t.Errorf("Port = %q, want 9090", c.Port)
	}
	if c.Provider.BaseURL != "http://ollama.example:11434" {
		t.Errorf("Provider.BaseURL = %q, want http://ollama.example:11434", c.Provider.BaseURL)
	}
	if c.AuthToken != "secret-xyz" {
		t.Errorf("AuthToken = %q, want secret-xyz", c.AuthToken)
	}
	if !c.Webhook.AllowHTTP {
		t.Errorf("Webhook.AllowHTTP = false, want true")
	}
	if !c.Webhook.AllowPrivate {
		t.Errorf("Webhook.AllowPrivate = false, want true")
	}
}

func TestLoadFromEnv_NoOverridesUsesDefaults(t *testing.T) {
	t.Setenv("PORT", "")
	c, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv: %v", err)
	}
	if c.Port != "8080" {
		t.Errorf("Port = %q, want default 8080", c.Port)
	}
}

func TestValidate_RejectsBadScoreWeights(t *testing.T) {
	c := Defaults()
	c.Score.Weights = map[string]int{"transport": 50, "quality": 30}
	if err := c.Validate(); err == nil {
		t.Fatal("expected validation error for weights summing to 80, got nil")
	}
}

func TestValidate_RejectsZeroBulkhead(t *testing.T) {
	c := Defaults()
	c.Limits.InteractiveSlots = 0
	if err := c.Validate(); err == nil {
		t.Fatal("expected error for InteractiveSlots=0")
	}
}

func TestValidate_RejectsNegativeLatencyTarget(t *testing.T) {
	c := Defaults()
	c.Score.LatencyP95Target = 0
	if err := c.Validate(); err == nil {
		t.Fatal("expected error for LatencyP95Target=0")
	}
}

func TestValidate_PassesOnDefaults(t *testing.T) {
	if err := Defaults().Validate(); err != nil {
		t.Fatalf("Defaults() should validate cleanly, got %v", err)
	}
}
