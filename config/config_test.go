package config

import (
	"log/slog"
	"testing"
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
