package telemetry

// Store bundles all telemetry subsystems behind a single field on the Agent.
type Store struct {
	Costs   *CostTracker
	Latency *LatencyTracker
	Webhook *WebhookDispatcher
}

// NewStore wires up cost tracking, latency tracking, and the webhook dispatcher.
func NewStore() *Store {
	return &Store{
		Costs:   NewCostTracker(),
		Latency: NewLatencyTracker(),
		Webhook: NewWebhookDispatcher(),
	}
}

// Stop is a no-op for now — all current telemetry stores are passive.
// Reserved for future use (e.g., closing OTEL exporters).
func (s *Store) Stop() {}
