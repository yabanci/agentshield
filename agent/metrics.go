package agent

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	requestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "agentshield_requests_total",
		Help: "Total requests by tier",
	}, []string{"tier"})

	requestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "agentshield_request_duration_seconds",
		Help:    "Request latency by tier",
		Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30},
	}, []string{"tier"})

	cbStateGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "agentshield_cb_state",
		Help: "Circuit breaker state: 0=closed 1=half-open 2=open",
	}, []string{"model"})

	cacheSizeGauge = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "agentshield_cache_size",
		Help: "Number of entries in the semantic cache",
	})

	cacheHitsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "agentshield_cache_hits_total",
		Help: "Semantic cache hits",
	})

	loadshedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "agentshield_loadshed_total",
		Help: "Requests rejected by load shedder",
	})

	bulkheadFullTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "agentshield_bulkhead_full_total",
		Help: "Requests rejected by bulkhead",
	}, []string{"type"})

	hedgeFiresTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "agentshield_hedge_fires_total",
		Help: "Number of times a hedge request was fired",
	})
)

func cbStateValue(s string) float64 {
	switch s {
	case "closed":
		return 0
	case "half-open":
		return 1
	default:
		return 2
	}
}
