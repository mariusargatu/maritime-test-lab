//go:build contract

package contract_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/sr"

	"maritime-test-lab/internal/repopath"
	"maritime-test-lab/internal/testfix"
	"maritime-test-lab/schemas"
)

// The seeded Schema Registry FULL-compat gate: seed the baseline from the
// committed schemas, set FULL on both subjects, then prove the current schemas
// pass AND a known-breaking fixture is rejected. FULL (not BACKWARD) is the right
// mode for a producer-first topology — BACKWARD would permit exactly the break a
// producer-first deploy ships (D-008).
func TestSchemaRegistryFullCompat(t *testing.T) {
	ctx := context.Background()

	_, srURL, stop, err := testfix.StartRedpanda(ctx)
	require.NoError(t, err)
	t.Cleanup(stop)

	client, err := sr.NewClient(sr.URLs(srURL))
	require.NoError(t, err)

	subjects := []struct{ subject, schema string }{
		{schemas.VoyageCreatedSubject, schemas.VoyageCreated},
		{schemas.EstimateReadySubject, schemas.EstimateReady},
	}
	for _, s := range subjects {
		_, err := client.CreateSchema(ctx, s.subject, sr.Schema{Schema: s.schema, Type: sr.TypeAvro})
		require.NoError(t, err, "seed %s", s.subject)
		for _, res := range client.SetCompatibility(ctx, sr.SetCompatibility{Level: sr.CompatFull}, s.subject) {
			require.NoError(t, res.Err, "set FULL on %s", s.subject)
		}
	}

	t.Run("current schemas are FULL-compatible with the baseline", func(t *testing.T) {
		for _, s := range subjects {
			res, err := client.CheckCompatibility(ctx, s.subject, -1, sr.Schema{Schema: s.schema, Type: sr.TypeAvro})
			require.NoError(t, err)
			assert.True(t, res.Is, "%s should be compatible: %v", s.subject, res.Messages)
		}
	})

	t.Run("red self-test: a breaking schema is rejected", func(t *testing.T) {
		breakingBytes, err := repopath.Read("schemas/testdata/breaking_voyage_created.avsc")
		require.NoError(t, err)
		res, err := client.CheckCompatibility(ctx, schemas.VoyageCreatedSubject, -1, sr.Schema{Schema: string(breakingBytes), Type: sr.TypeAvro})
		require.NoError(t, err)
		assert.False(t, res.Is, "the breaking fixture must be rejected — a gate that can't fail is decoration")
	})
}
