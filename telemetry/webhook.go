// webhook.go — fires HTTP notifications on circuit breaker state changes.
package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
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

// maxInFlightWebhookFires caps concurrent webhook POSTs so an attacker
// who can drive rapid CB state transitions (e.g. chaos cycling) can't
// fan out an unbounded goroutine swarm — both a server-side resource
// spike and an amplifier against the configured webhook target.
const maxInFlightWebhookFires = 32

// WebhookDispatcher sends events to a configurable HTTP endpoint.
// All dispatches are non-blocking (goroutine + timeout), capped by a
// semaphore so the goroutine count stays bounded under burst.
type WebhookDispatcher struct {
	mu     sync.RWMutex
	url    string
	client *http.Client
	sem    chan struct{}
}

func NewWebhookDispatcher() *WebhookDispatcher {
	return &WebhookDispatcher{
		client: &http.Client{
			Timeout: 5 * time.Second,
			// Refuse redirects: a 301/302 to an internal IP (e.g.
			// http://10.0.0.1/) would bypass the validation done at SetURL
			// time. Webhook receivers don't legitimately need redirects.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		sem: make(chan struct{}, maxInFlightWebhookFires),
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

	// Drop the fire (return immediately) if the in-flight cap is full.
	// Webhooks are best-effort observability — preferable to a goroutine
	// fan-out spike during a chaos burst. The metric below records drops
	// so operators can detect tuning issues.
	select {
	case w.sem <- struct{}{}:
	default:
		WebhookDroppedTotal.Inc()
		return
	}

	go func() {
		defer func() { <-w.sem }()

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
		// Drain the body so the underlying TCP connection can be reused.
		// Without this, Go's HTTP pool keeps the connection in TIME_WAIT.
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
}

