package orchestrator

import (
	"github.com/yabanci/flowguard/circuitbreaker"

	"github.com/yabanci/agentshield/quality"
)

// BreakerSet bundles the transport + semantic circuit breakers for both primary
// and fallback models. Keeping the four breakers as one unit avoids passing
// them around as four separate parameters and clarifies the invariant: each
// model has exactly one transport CB and exactly one semantic CB.
type BreakerSet struct {
	PrimaryTransport  *circuitbreaker.Breaker
	FallbackTransport *circuitbreaker.Breaker
	PrimarySemantic   *quality.SemanticBreaker
	FallbackSemantic  *quality.SemanticBreaker
}

// NewBreakerSet wires four pre-constructed breakers into one set.
// Caller is responsible for the configuration of each individual breaker.
func NewBreakerSet(
	primaryTransport, fallbackTransport *circuitbreaker.Breaker,
	primarySemantic, fallbackSemantic *quality.SemanticBreaker,
) *BreakerSet {
	return &BreakerSet{
		PrimaryTransport:  primaryTransport,
		FallbackTransport: fallbackTransport,
		PrimarySemantic:   primarySemantic,
		FallbackSemantic:  fallbackSemantic,
	}
}
