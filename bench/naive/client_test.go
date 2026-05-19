package naive_test

import (
	"context"
	"testing"

	"github.com/yabanci/agentshield/bench/fakebackend"
	"github.com/yabanci/agentshield/bench/naive"
)

func TestNaiveClient_Garbage_ReturnsGarbageText(t *testing.T) {
	srv := fakebackend.New(42)
	defer srv.Close()

	c := naive.New(srv.URL(), "test", naive.WithScenario("garbage"))
	text, err := c.Generate(context.Background(), "test prompt for naive client garbage")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The response should be non-empty (naive returns whatever the model says).
	if text == "" {
		t.Fatal("expected non-empty response even for garbage scenario")
	}
}

func TestNaiveClient_Down_ReturnsError(t *testing.T) {
	srv := fakebackend.New(42)
	defer srv.Close()

	c := naive.New(srv.URL(), "test",
		naive.WithScenario("down"),
		naive.WithMaxRetries(1),
	)
	_, err := c.Generate(context.Background(), "test prompt for naive client down")
	if err == nil {
		t.Fatal("expected error for down scenario, got nil")
	}
}

func TestNaiveClient_NoScenario_ReturnsGoodText(t *testing.T) {
	srv := fakebackend.New(42)
	defer srv.Close()

	c := naive.New(srv.URL(), "test")
	text, err := c.Generate(context.Background(), "Explain what a circuit breaker does in software systems.")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text == "" {
		t.Fatal("expected non-empty text from no-scenario call")
	}
	// Good response should be at least 50 chars (all good fixtures are much longer).
	if len(text) < 50 {
		t.Errorf("expected at least 50 chars, got %d: %q", len(text), text)
	}
}

func TestNaiveClient_RetryExhaustion_ReturnsError(t *testing.T) {
	srv := fakebackend.New(42)
	defer srv.Close()

	// maxRetries=0 means 1 total attempt (the initial try with no retries).
	c := naive.New(srv.URL(), "test",
		naive.WithScenario("down"),
		naive.WithMaxRetries(0),
	)
	_, err := c.Generate(context.Background(), "any prompt")
	if err == nil {
		t.Fatal("expected error with 0 retries against down backend")
	}
}
