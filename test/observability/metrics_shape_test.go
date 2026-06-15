// Package observability holds the test that ties the monitor's alert rules to the
// metrics the services actually emit. promtool tests the rules against SYNTHETIC
// series (make lint); the metricnames guard pins the names; but neither proves the
// running code exposes those names with the right Prometheus TYPE. A latency rule
// that does histogram_quantile(...latency_seconds_bucket...) is silently dead if
// the metric isn't a histogram — no _bucket series exists. This catches that.
package observability

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"maritime-test-lab/internal/metricnames"

	// Blank-imported so their promauto metrics register on the default registry.
	_ "maritime-test-lab/services/voyage/adapters/kafka"
	_ "maritime-test-lab/services/voyage/adapters/postgres"
)

func TestPipelineMetricsMatchRuleShape(t *testing.T) {
	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)

	byName := make(map[string]*dto.MetricFamily, len(families))
	for _, f := range families {
		byName[f.GetName()] = f
	}

	// name → the Prometheus type the alert rules assume. The latency rule needs a
	// histogram (its _bucket series); the rest are scalar counters/gauges.
	want := []struct {
		name string
		typ  dto.MetricType
	}{
		{metricnames.CreateDedupe, dto.MetricType_COUNTER},
		{metricnames.EstimateApplied, dto.MetricType_COUNTER},
		{metricnames.EstimateErrors, dto.MetricType_COUNTER},
		{metricnames.EstimateLatency, dto.MetricType_HISTOGRAM},
		{metricnames.DLQDepth, dto.MetricType_GAUGE},
		{metricnames.ConsumerLag, dto.MetricType_GAUGE},
	}

	for _, w := range want {
		t.Run(w.name, func(t *testing.T) {
			fam, ok := byName[w.name]
			require.Truef(t, ok, "%s is referenced by the alert rules but the code emits no such series", w.name)
			assert.Equal(t, w.typ, fam.GetType(), "%s must be a %s for its rule to evaluate", w.name, w.typ)

			if w.typ == dto.MetricType_HISTOGRAM {
				require.NotEmpty(t, fam.GetMetric(), "%s has no metric", w.name)
				assert.NotEmpty(t, fam.GetMetric()[0].GetHistogram().GetBucket(),
					"%s must expose _bucket series — the latency rule's histogram_quantile depends on it", w.name)
			}
		})
	}
}
