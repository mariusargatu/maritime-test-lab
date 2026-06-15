package postgres

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"maritime-test-lab/internal/metricnames"
)

// dedupeTotal counts CreateVoyage calls that returned an existing voyage (the
// idempotency path). Package-level by Prometheus convention; the name comes from
// the shared metricnames consts the monitor's alert rules also reference.
var dedupeTotal = promauto.NewCounter(prometheus.CounterOpts{
	Name: metricnames.CreateDedupe,
	Help: "CreateVoyage calls that returned an existing voyage (idempotent replays).",
})
