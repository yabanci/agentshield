package telemetry_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/yabanci/agentshield/telemetry"
	"github.com/yabanci/agentshield/quality"
)

func TestWebhook_FiredOnSemanticCBStateChange(t *testing.T) {
	var mu sync.Mutex
	var received []telemetry.WebhookEvent
	done := make(chan struct{})

	// Create a test webhook server
	webhookSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ev telemetry.WebhookEvent
		if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
			t.Errorf("failed to decode webhook event: %v", err)
			return
		}
		mu.Lock()
		alreadyDone := len(received) > 0
		received = append(received, ev)
		mu.Unlock()
		if !alreadyDone {
			close(done)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer webhookSrv.Close()

	// Set up breaker with short timeout and fast trip
	cfg := quality.SemanticBreakerConfig{
		WindowSize:        4,
		MinSamples:        2,
		DegradedThreshold: 0.65,
		FailingThreshold:  0.45,
		OpenTimeout:       60 * time.Second,
		RecoverySamples:   2,
	}

	dispatcher := telemetry.NewTestWebhookDispatcher()
	dispatcher.SetURL(webhookSrv.URL)

	sb := quality.NewSemanticBreaker(cfg)
	sb.WithStateChangeCallback(func(prev, next quality.SBState, reason string, avg float64) {
		dispatcher.Fire(telemetry.WebhookEvent{
			Event:      "semantic_cb_" + string(next),
			Model:      "primary",
			PrevState:  string(prev),
			NewState:   string(next),
			Reason:     reason,
			AvgQuality: avg,
			Timestamp:  time.Now(),
		})
	})

	// Record bad scores to trip the breaker
	for i := 0; i < 3; i++ {
		sb.Record(0.10, quality.QualityResult{Score: 0.10})
	}

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("webhook never fired")
	}

	mu.Lock()
	for _, ev := range received {
		if ev.Event == "" {
			t.Error("webhook event should have non-empty event name")
		}
		if ev.Model != "primary" {
			t.Errorf("expected model=primary, got %s", ev.Model)
		}
	}
	mu.Unlock()
}

func TestWebhook_NoFireWhenURLNotSet(t *testing.T) {
	fired := make(chan struct{}, 1)
	webhookSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case fired <- struct{}{}:
		default:
		}
	}))
	defer webhookSrv.Close()

	dispatcher := telemetry.NewTestWebhookDispatcher()
	// No URL set

	dispatcher.Fire(telemetry.WebhookEvent{Event: "test"})
	select {
	case <-fired:
		t.Error("webhook should not fire when URL is not set")
	case <-time.After(100 * time.Millisecond):
		// Expected: no fire within the window.
	}
}

func TestWebhook_ConfigurableURL(t *testing.T) {
	d := telemetry.NewTestWebhookDispatcher()
	if d.URL() != "" {
		t.Error("URL should be empty initially")
	}
	d.SetURL("https://example.com/hook")
	if d.URL() != "https://example.com/hook" {
		t.Errorf("expected URL to be set, got %s", d.URL())
	}
	d.ClearURL()
	if d.URL() != "" {
		t.Error("URL should be empty after clear")
	}
}
