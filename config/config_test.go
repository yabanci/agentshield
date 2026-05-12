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
