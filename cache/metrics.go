package cache

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	cacheSizeGauge = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "agentshield_cache_size",
		Help: "Number of entries in the semantic cache",
	})

	cacheHitsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "agentshield_cache_hits_total",
		Help: "Semantic cache hits",
	})
)
