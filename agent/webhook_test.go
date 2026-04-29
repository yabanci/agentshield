package agent_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/yabanci/agentshield/agent"
)

func TestWebhook_FiredOnSemanticCBStateChange(t *testing.T) {
	var mu sync.Mutex
	var received []agent.WebhookEvent

	// Create a test webhook server
	webhookSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ev agent.WebhookEvent
		if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
			t.Errorf("failed to decode webhook event: %v", err)
			return
		}
		mu.Lock()
		received = append(received, ev)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer webhookSrv.Close()

	// Set up breaker with short timeout and fast trip
	cfg := agent.SemanticBreakerConfig{
		WindowSize:        4,
		MinSamples:        2,
		DegradedThreshold: 0.65,
		FailingThreshold:  0.45,
		OpenTimeout:       60 * time.Second,
		RecoverySamples:   2,
	}

	dispatcher := agent.NewTestWebhookDispatcher()
	dispatcher.SetURL(webhookSrv.URL)

	sb := agent.NewSemanticBreaker(cfg)
	sb.WithStateChangeCallback(func(prev, next agent.SBState, reason string, avg float64) {
		dispatcher.Fire(agent.WebhookEvent{
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
		sb.Record(0.10, agent.QualityResult{Score: 0.10})
	}

	// Give webhook goroutine time to fire
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	count := len(received)
	mu.Unlock()

	if count == 0 {
		t.Error("expected at least one webhook event after CB state change")
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
	fired := false
	webhookSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fired = true
	}))
	defer webhookSrv.Close()

	dispatcher := agent.NewTestWebhookDispatcher()
	// No URL set

	dispatcher.Fire(agent.WebhookEvent{Event: "test"})
	time.Sleep(50 * time.Millisecond)

	if fired {
		t.Error("webhook should not fire when URL is not set")
	}
}

func TestWebhook_ConfigurableURL(t *testing.T) {
	d := agent.NewTestWebhookDispatcher()
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
