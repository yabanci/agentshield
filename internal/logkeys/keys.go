// Package logkeys defines stable structured-log field names.
// Dashboards and alerts in HyperDX/Loki/CloudWatch key off these — never
// inline a literal string in a log call when a constant exists here.
//
// Naming convention: lower_snake_case. New keys go here, not in service files.
package logkeys

const (
	Component    = "component"
	TraceID      = "trace_id"
	SessionID    = "session_id"
	Tier         = "tier"
	Model        = "model"
	Outcome      = "outcome"
	LatencyMS    = "latency_ms"
	QualityScore = "quality_score"
	CBState      = "cb_state"
	Err          = "err"
)
