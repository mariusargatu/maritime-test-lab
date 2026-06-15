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

// The outbox poller publishes a created voyage as a decodable voyage.created
// event — the producer half of the async loop, proven in isolation.
func TestOutboxPollerPublishesVoyageCreated(t *testing.T) {
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
	repo := voyagepg.NewRepository(pool, vkafka.VoyageCreatedEncoder(serde))

	producer, err := kgo.NewClient(kgo.SeedBrokers(brokers))
	require.NoError(t, err)
	t.Cleanup(producer.Close)

	consumer, err := kgo.NewClient(
		kgo.SeedBrokers(brokers),
		kgo.ConsumeTopics(schemas.TopicVoyageCreated),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
	)
	require.NoError(t, err)
	t.Cleanup(consumer.Close)

	id := uuid.NewString()
	_, err = repo.Create(ctx, domain.Voyage{ClientRequestID: id, Origin: "NLRTM", Dest: "SGSIN", DistanceNm: 8200, FeesMinor: 5000})
	require.NoError(t, err)
	require.NoError(t, vkafka.NewPoller(pool, producer, time.Second).Flush(ctx))

	got := pollVoyageCreated(t, consumer, serde, id, 15*time.Second)
	assert.Equal(t, avro.VoyageCreated{
		ClientRequestID: id, Origin: "NLRTM", Dest: "SGSIN",
		DistanceNm: 8200, FeesMinor: 5000, Version: 1,
	}, got)
}

// A crash between the broker ack and the published_at stamp must re-publish, not
// drop: flag-on-ack is deliberately at-least-once (D-005). We reproduce the exact
// residual state — a row that was acked but never flagged — by nulling published_at
// after a successful Flush, then prove the next Flush re-emits it. Downstream the
// versioned idempotent upsert (TestUpsertEstimate) absorbs the duplicate; together
// they are the outbox↔consumer correctness pair.
func TestOutboxRepublishesAfterCrashWindow(t *testing.T) {
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
	repo := voyagepg.NewRepository(pool, vkafka.VoyageCreatedEncoder(serde))

	producer, err := kgo.NewClient(kgo.SeedBrokers(brokers))
	require.NoError(t, err)
	t.Cleanup(producer.Close)

	id := uuid.NewString()
	_, err = repo.Create(ctx, domain.Voyage{ClientRequestID: id, Origin: "NLRTM", Dest: "SGSIN", DistanceNm: 8200, FeesMinor: 5000})
	require.NoError(t, err)

	poller := vkafka.NewPoller(pool, producer, time.Second)
	require.NoError(t, poller.Flush(ctx)) // publishes + stamps published_at

	// The crash window: acked, but the stamp never persisted. Re-arm the row.
	_, err = pool.Exec(ctx, "UPDATE outbox SET published_at = NULL")
	require.NoError(t, err)
	require.NoError(t, poller.Flush(ctx)) // must re-publish the same event

	copies := countVoyageCreated(t, brokers, serde, id, 12*time.Second)
	assert.GreaterOrEqual(t, copies, 2, "the re-armed row is re-published — at-least-once, never lost")
}

// countVoyageCreated reads the topic from the start and counts events for id over
// a fixed window (a fresh group each call so offsets never leak between tests).
func countVoyageCreated(t *testing.T, brokers string, serde *avroserde.Serde, id string, window time.Duration) int {
	t.Helper()
	cl, err := kgo.NewClient(
		kgo.SeedBrokers(brokers),
		kgo.ConsumeTopics(schemas.TopicVoyageCreated),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
	)
	require.NoError(t, err)
	t.Cleanup(cl.Close)

	count := 0
	deadline := time.Now().Add(window)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		fetches := cl.PollFetches(ctx)
		cancel()
		fetches.EachRecord(func(r *kgo.Record) {
			var vc avro.VoyageCreated
			if err := serde.Decode(r.Value, &vc); err == nil && vc.ClientRequestID == id {
				count++
			}
		})
	}
	return count
}

func pollVoyageCreated(t *testing.T, cl *kgo.Client, serde *avroserde.Serde, id string, timeout time.Duration) avro.VoyageCreated {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		fetches := cl.PollFetches(ctx)
		cancel()

		var found *avro.VoyageCreated
		fetches.EachRecord(func(r *kgo.Record) {
			var vc avro.VoyageCreated
			if err := serde.Decode(r.Value, &vc); err == nil && vc.ClientRequestID == id {
				found = &vc
			}
		})
		if found != nil {
			return *found
		}
	}
	t.Fatalf("voyage.created for %s not seen within %s", id, timeout)
	return avro.VoyageCreated{}
}
