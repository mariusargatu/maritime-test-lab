package metricnames_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"maritime-test-lab/internal/metricnames"
	"maritime-test-lab/internal/repopath"
)

// The estimate-pipeline alert rules must reference the shared metric-name consts.
// Renaming a const without updating the rules fails here — so the rules can never
// silently alert on a metric the services no longer emit (D-046).
func TestRuleFilesUseMetricNames(t *testing.T) {
	data, err := repopath.Read("observability/rules/estimate_pipeline.yaml")
	require.NoError(t, err)
	rules := string(data)

	for _, name := range []string{
		metricnames.EstimateApplied,
		metricnames.EstimateErrors,
		metricnames.EstimateLatency,
		metricnames.DLQDepth,
		metricnames.ConsumerLag,
	} {
		assert.Contains(t, rules, name, "alert rules must reference %s", name)
	}
}
