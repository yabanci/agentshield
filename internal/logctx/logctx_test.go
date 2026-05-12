package logctx_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/yabanci/agentshield/internal/logctx"
)

func TestRoundTrip(t *testing.T) {
	want := slog.Default().With("trace_id", "abc-123")
	ctx := logctx.With(context.Background(), want)
	got := logctx.From(ctx)
	if got != want {
		t.Error("From returned different *slog.Logger than With stored")
	}
}

func TestFromMissingReturnsDefault(t *testing.T) {
	got := logctx.From(context.Background())
	if got == nil {
		t.Fatal("From returned nil for ctx without logger; want a usable default")
	}
}
