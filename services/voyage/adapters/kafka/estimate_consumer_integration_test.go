//go:build integration

package kafka_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"

	"maritime-test-lab/gen/avro"
	"maritime-test-lab/internal/avroserde"
	"maritime-test-lab/internal/eventreg"
	"maritime-test-lab/internal/testfix"
	"maritime-test-lab/schemas"
	vkafka "maritime-test-lab/services/voyage/adapters/kafka"
	voyagepg "maritime-test-lab/services/voyage/adapters/postgres"
	"maritime-test-lab/services/voyage/domain"
)

// The versioned idempotent upsert survives redelivery and out-of-order versions:
// the estimate matching the voyage's version is applied exactly once; an event
// for a different version is a no-op. Order-independent.
func TestEstimateConsumerVersionedUpsert(t *testing.T) {
	ctx := context.Background()

	dsn, stopPG, err := testfix.StartPostgres(ctx)
	require.NoError(t, err)
	t.Cleanup(stopPG)
	brokers, srURL, stopRP, err := testfix.StartRedpanda(ctx)
	require.NoError(t, err)
	t.Cleanup(stopRP)

	serde, err := avroserde.New(ctx, srURL, eventreg.All()...)
	require.NoError(t, err)

	pool, err := voyagepg.Connect(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	repo := voyagepg.NewRepository(pool, func(domain.Voyage) (string, []byte, error) {
		return schemas.TopicVoyageCreated, []byte("evt"), nil
	})

	// a voyage at version 1
	id := uuid.NewString()
	created, err := repo.Create(ctx, domain.Voyage{ClientRequestID: id, Origin: "NLRTM", Dest: "SGSIN", DistanceNm: 8200, FeesMinor: 5000})
	require.NoError(t, err)
	require.Equal(t, int64(1), created.Version)

	consumerClient, err := kgo.NewClient(
		kgo.SeedBrokers(brokers),
		kgo.ConsumeTopics(schemas.TopicEstimateReady),
		kgo.ConsumerGroup(vkafka.EstimateConsumerGroup),
		kgo.DisableAutoCommit(),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
	)
	require.NoError(t, err)
	t.Cleanup(consumerClient.Close)
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go vkafka.NewEstimateConsumer(consumerClient, serde, repo).Run(cctx)

	producer, err := kgo.NewClient(kgo.SeedBrokers(brokers))
	require.NoError(t, err)
	t.Cleanup(producer.Close)

	const matching = int64(11111) // version 1 — the voyage's actual version
	const stale = int64(22222)    // version 2 — must be ignored

	// out-of-order: stale (v2) first, then the matching (v1) twice (redelivery)
	publishEstimate(t, ctx, producer, serde, id, 2, stale)
	publishEstimate(t, ctx, producer, serde, id, 1, matching)
	publishEstimate(t, ctx, producer, serde, id, 1, matching)

	require.Eventually(t, func() bool {
		got, err := repo.Get(ctx, id)
		return err == nil && got.EstimateMinor == matching
	}, 25*time.Second, 200*time.Millisecond, "matching-version estimate should apply")

	// and it stays correct — the stale version never overwrote it
	got, err := repo.Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, matching, got.EstimateMinor)
}

func publishEstimate(t *testing.T, ctx context.Context, p *kgo.Client, serde *avroserde.Serde, voyageID string, version, amount int64) {
	t.Helper()
	payload, err := serde.Encode(avro.EstimateReady{
		VoyageID: voyageID, VoyageVersion: version, AmountMinor: amount, Currency: "USD",
		CalculatedAt: time.UnixMilli(1_700_000_000_000).UTC(),
	})
	require.NoError(t, err)
	require.NoError(t, p.ProduceSync(ctx, &kgo.Record{Topic: schemas.TopicEstimateReady, Value: payload}).FirstErr())
}
