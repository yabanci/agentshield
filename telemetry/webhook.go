// webhook.go — fires HTTP notifications on circuit breaker state changes.
package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// WebhookEvent is the payload sent to the configured webhook URL.
type WebhookEvent struct {
	Event      string    `json:"event"`
	Model      string    `json:"model"`
	PrevState  string    `json:"prev_state"`
	NewState   string    `json:"new_state"`
	Reason     string    `json:"reason,omitempty"`
	AvgQuality float64   `json:"avg_quality"`
	Timestamp  time.Time `json:"timestamp"`
}

// WebhookDispatcher sends events to a configurable HTTP endpoint.
// All dispatches are non-blocking (goroutine + timeout).
type WebhookDispatcher struct {
	mu     sync.RWMutex
	url    string
	client *http.Client
}

func NewWebhookDispatcher() *WebhookDispatcher {
	return &WebhookDispatcher{
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

// NewTestWebhookDispatcher creates a dispatcher for use in tests.
func NewTestWebhookDispatcher() *WebhookDispatcher {
	return NewWebhookDispatcher()
}

func (w *WebhookDispatcher) SetURL(url string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.url = url
}

func (w *WebhookDispatcher) ClearURL() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.url = ""
}

func (w *WebhookDispatcher) URL() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.url
}

// Fire sends the event asynchronously. Never blocks the caller.
func (w *WebhookDispatcher) Fire(event WebhookEvent) {
	w.mu.RLock()
	url := w.url
	w.mu.RUnlock()

	if url == "" {
		return
	}

	go func() {
		body, err := json.Marshal(event)
		if err != nil {
			return
		}
		req, err := http.NewRequestWithContext(
			context.Background(), http.MethodPost, url, bytes.NewReader(body),
		)
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "AgentShield/1.0")

		resp, err := w.client.Do(req)
		if err != nil {
			return
		}
		defer resp.Body.Close() //nolint:errcheck
	}()
}

