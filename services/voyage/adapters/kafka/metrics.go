package kafka

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"maritime-test-lab/internal/metricnames"
)

// The estimate-pipeline metrics the monitor's alert rules fire on. Emitted live
// by the voyage consumer + the lag reporter; names come from internal/metricnames
// (the same consts the rules reference and the guard test asserts).
var (
	estimateApplied = promauto.NewCounter(prometheus.CounterOpts{
		Name: metricnames.EstimateApplied,
		Help: "estimate.ready events applied to a voyage (pipeline throughput).",
	})
	estimateErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: metricnames.EstimateErrors,
		Help: "estimate.ready processing failures (decode or apply).",
	})
	estimateLatency = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    metricnames.EstimateLatency,
		Help:    "create-to-estimate end-to-end latency in seconds.",
		Buckets: prometheus.DefBuckets,
	})
	dlqDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Name: metricnames.DLQDepth,
		Help: "estimate.ready dead-letter entries seen.",
	})
	consumerLag = promauto.NewGauge(prometheus.GaugeOpts{
		Name: metricnames.ConsumerLag,
		Help: "voyage consumer-group lag (dashboard surface).",
	})
)
