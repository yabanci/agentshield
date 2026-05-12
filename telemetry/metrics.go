package telemetry

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/yabanci/agentshield/quality"
)

// All Prometheus collectors are package-level exported so other packages
// (agent, orchestrator in F3b) can update them via getter constants.
//
// Naming: agentshield_<subsystem>_<metric>. Promauto registers with the default
// registerer automatically.
var (
	RequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "agentshield_requests_total",
		Help: "Total requests by tier",
	}, []string{"tier"})

	RequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "agentshield_request_duration_seconds",
		Help:    "Request latency by tier",
		Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30},
	}, []string{"tier"})

	CBStateGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "agentshield_cb_state",
		Help: "Circuit breaker state: 0=closed 1=half-open 2=open",
	}, []string{"model"})

	// cache metrics live in cache/metrics.go (cache owns its own observability)

	LoadshedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "agentshield_loadshed_total",
		Help: "Requests rejected by load shedder",
	})

	BulkheadFullTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "agentshield_bulkhead_full_total",
		Help: "Requests rejected by bulkhead",
	}, []string{"type"})

	HedgeFiresTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "agentshield_hedge_fires_total",
		Help: "Number of times a hedge request was fired",
	})

	QualityGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "agentshield_quality_score",
		Help: "Latest semantic quality score per model (0=trash, 1=perfect)",
	}, []string{"model"})

	SemanticCBStateGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "agentshield_semantic_cb_state",
		Help: "Semantic circuit breaker state: 0=healthy 1=degraded 2=failing",
	}, []string{"model"})
)

// CBStateValue converts a transport-breaker state string to a metric value.
func CBStateValue(s string) float64 {
	switch s {
	case "closed":
		return 0
	case "half-open":
		return 1
	default:
		return 2
	}
}

// SBStateValue converts a semantic-breaker state to a metric value.
func SBStateValue(s quality.SBState) float64 {
	switch s {
	case quality.SBHealthy:
		return 0
	case quality.SBDegraded:
		return 1
	default: // quality.SBFailing
		return 2
	}
}
