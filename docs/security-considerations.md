# Security considerations

## OTel span attributes and PII

When an OTel endpoint is configured, AgentShield captures tool inputs as span
attributes (truncated to 2 KB). This means raw user input — including anything
passed as a tool argument — flows to the configured tracing backend.

**Trade-off:** Losing tool inputs from traces removes a primary debugging signal.
Keeping them risks exposing PII or credentials if the tracing backend is shared
across tenants or not access-controlled.

**Recommendation:** Before pointing AgentShield at a shared collector, audit
which tools receive user-controlled input and whether that input can contain
PII, credentials, or secrets. Apply column-level redaction in the collector
pipeline (e.g., OpenTelemetry Collector `transform` processor) if needed.

A startup INFO log is emitted whenever an OTel endpoint is configured, reminding
operators of this trade-off. A WARN is emitted if the connection is insecure
(plaintext gRPC) — see `OTEL_EXPORTER_OTLP_INSECURE`.
