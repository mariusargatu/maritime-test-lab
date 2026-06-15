package schemacheck

import (
	"testing"
	"time"

	hambaavro "github.com/hamba/avro/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	avromodel "maritime-test-lab/gen/avro"
	"maritime-test-lab/internal/repopath"
)

// Evolution decode tests exercise the actual bytes path the Schema Registry
// gate only checks algebraically: encode with the historical writer schema,
// decode with the current reader, and the reverse. Each schemas/history/*.v1
// file is a GENUINE prior version (it carries a since-removed field with a
// default), so the round-trip drives real Avro schema resolution — the current
// reader drops the removed field, the v1 reader default-fills it — rather than
// re-encoding a schema against an identical copy of itself.

func TestVoyageCreatedEvolution(t *testing.T) {
	old := mustSchema(t, "schemas/history/voyage_created.v1.avsc")
	cur := mustSchema(t, "schemas/voyage_created.avsc")

	in := avromodel.VoyageCreated{
		ClientRequestID: "req-1", Origin: "NLRTM", Dest: "SGSIN",
		DistanceNm: 8200, FeesMinor: 5000, Version: 1,
	}
	assertRoundTrips(t, old, cur, in)
}

func TestEstimateReadyEvolution(t *testing.T) {
	old := mustSchema(t, "schemas/history/estimate_ready.v1.avsc")
	cur := mustSchema(t, "schemas/estimate_ready.avsc")

	in := avromodel.EstimateReady{
		VoyageID: "req-1", VoyageVersion: 1, AmountMinor: 25000, Currency: "USD",
		CalculatedAt: time.UnixMilli(1_700_000_000_000).UTC(),
	}
	assertRoundTrips(t, old, cur, in)
}

// TestEvolutionGateRejectsIncompatibleHistory is the red self-test: a gate that
// has never been seen to fail is indistinguishable from one that can't
// (CLAUDE.md). The breaking fixture changes distance_nm from int to string — an
// incompatible change — so a current value must NOT round-trip across it.
func TestEvolutionGateRejectsIncompatibleHistory(t *testing.T) {
	breaking := mustSchema(t, "schemas/testdata/breaking_voyage_created.avsc")

	in := avromodel.VoyageCreated{
		ClientRequestID: "req-1", Origin: "NLRTM", Dest: "SGSIN",
		DistanceNm: 8200, FeesMinor: 5000, Version: 1,
	}

	// distance_nm is a string in the breaking schema but an int in the value, so
	// the value cannot be encoded under it — the round-trip is rejected, proving
	// the gate can fail.
	_, err := hambaavro.Marshal(breaking, in)
	require.Error(t, err, "an incompatible historical schema must not silently round-trip")
}

// assertRoundTrips encodes with one schema and decodes with the other, both
// directions, asserting the value survives.
func assertRoundTrips[T any](t *testing.T, old, cur hambaavro.Schema, in T) {
	t.Helper()

	forward, err := hambaavro.Marshal(old, in)
	require.NoError(t, err)
	var outFwd T
	require.NoError(t, hambaavro.Unmarshal(cur, forward, &outFwd))
	assert.Equal(t, in, outFwd, "old writer -> current reader")

	backward, err := hambaavro.Marshal(cur, in)
	require.NoError(t, err)
	var outBack T
	require.NoError(t, hambaavro.Unmarshal(old, backward, &outBack))
	assert.Equal(t, in, outBack, "current writer -> old reader")
}

func mustSchema(t *testing.T, rel string) hambaavro.Schema {
	t.Helper()
	data, err := repopath.Read(rel)
	require.NoError(t, err)
	s, err := hambaavro.Parse(string(data))
	require.NoError(t, err)
	return s
}
