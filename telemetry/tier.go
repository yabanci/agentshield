package telemetry

// Tier is the canonical home of the tier label used across cost/latency/score.
// Other packages (agent, orchestrator) alias to this type to avoid duplication:
//   type Tier = telemetry.Tier
type Tier string

const (
	TierPrimary  Tier = "primary"
	TierFallback Tier = "fallback"
	TierCache    Tier = "cache"
	TierDegraded Tier = "degraded"
)
