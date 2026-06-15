//go:build integration

package avroserde_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"maritime-test-lab/gen/avro"
	"maritime-test-lab/internal/avroserde"
	"maritime-test-lab/internal/testfix"
	"maritime-test-lab/schemas"
)

func TestSerdeRoundTrip(t *testing.T) {
	ctx := context.Background()

	_, srURL, stop, err := testfix.StartRedpanda(ctx)
	require.NoError(t, err)
	t.Cleanup(stop)

	serde, err := avroserde.New(ctx, srURL,
		avroserde.Registration{Subject: schemas.VoyageCreatedSubject, Schema: schemas.VoyageCreated, Example: avro.VoyageCreated{}},
		avroserde.Registration{Subject: schemas.EstimateReadySubject, Schema: schemas.EstimateReady, Example: avro.EstimateReady{}},
	)
	require.NoError(t, err)

	t.Run("voyage.created round trips with confluent framing", func(t *testing.T) {
		in := avro.VoyageCreated{ClientRequestID: "req-1", Origin: "NLRTM", Dest: "SGSIN", DistanceNm: 8200, FeesMinor: 5000, Version: 1}

		b, err := serde.Encode(in)
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(b), 5, "magic byte + 4-byte schema id")
		assert.Equal(t, byte(0), b[0], "confluent magic byte")

		var out avro.VoyageCreated
		require.NoError(t, serde.Decode(b, &out))
		assert.Equal(t, in, out)
	})

	t.Run("estimate.ready round trips", func(t *testing.T) {
		in := avro.EstimateReady{VoyageID: "req-1", VoyageVersion: 1, AmountMinor: 25000, Currency: "USD", CalculatedAt: time.UnixMilli(1_700_000_000_000).UTC()}

		b, err := serde.Encode(in)
		require.NoError(t, err)

		var out avro.EstimateReady
		require.NoError(t, serde.Decode(b, &out))
		assert.Equal(t, in, out)
	})
}
