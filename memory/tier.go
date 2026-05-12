// Package memory holds session/trace/score-history storage.
// Tier label is aliased from telemetry to keep canonical naming and avoid duplicate types.
package memory

import "github.com/yabanci/agentshield/telemetry"

type Tier = telemetry.Tier

const (
	TierPrimary  = telemetry.TierPrimary
	TierFallback = telemetry.TierFallback
	TierCache    = telemetry.TierCache
	TierDegraded = telemetry.TierDegraded
)
