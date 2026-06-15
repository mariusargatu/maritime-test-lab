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
	"maritime-test-lab/internal/money"
	"maritime-test-lab/internal/testfix"
	"maritime-test-lab/schemas"
	estimatorkafka "maritime-test-lab/services/estimator/adapters/kafka"
)

// The estimator consumer prices a voyage.created and publishes a decodable
// estimate.ready — the middle of the async loop, proven in isolation.
func TestEstimatorConsumerPricesAndPublishes(t *testing.T) {
	ctx := context.Background()

	brokers, srURL, stopRP, err := testfix.StartRedpanda(ctx)
	require.NoError(t, err)
	t.Cleanup(stopRP)

	serde, err := avroserde.New(ctx, srURL, eventreg.All()...)
	require.NoError(t, err)

	consumerClient, err := kgo.NewClient(
		kgo.SeedBrokers(brokers),
		kgo.ConsumeTopics(schemas.TopicVoyageCreated),
		kgo.ConsumerGroup(estimatorkafka.ConsumerGroup),
		kgo.DisableAutoCommit(),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
	)
	require.NoError(t, err)
	t.Cleanup(consumerClient.Close)

	fixedTime := func() time.Time { return time.UnixMilli(1_700_000_000_000).UTC() }
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go estimatorkafka.NewConsumer(consumerClient, serde, money.FromUSD(200), fixedTime).Run(cctx)

	// Inject a voyage.created (distance 100 * rate 200 + fees 5000 = 25000).
	producer, err := kgo.NewClient(kgo.SeedBrokers(brokers))
	require.NoError(t, err)
	t.Cleanup(producer.Close)
	id := uuid.NewString()
	vcBytes, err := serde.Encode(avro.VoyageCreated{ClientRequestID: id, Origin: "NLRTM", Dest: "SGSIN", DistanceNm: 100, FeesMinor: 5000, Version: 1})
	require.NoError(t, err)
	require.NoError(t, producer.ProduceSync(ctx, &kgo.Record{Topic: schemas.TopicVoyageCreated, Value: vcBytes}).FirstErr())

	got := pollEstimateReady(t, brokers, serde, id, 25*time.Second)
	assert.Equal(t, avro.EstimateReady{
		VoyageID: id, VoyageVersion: 1, AmountMinor: 25000, Currency: "USD",
		CalculatedAt: fixedTime(),
	}, got)
}

func pollEstimateReady(t *testing.T, brokers string, serde *avroserde.Serde, id string, timeout time.Duration) avro.EstimateReady {
	t.Helper()
	verifier, err := kgo.NewClient(
		kgo.SeedBrokers(brokers),
		kgo.ConsumeTopics(schemas.TopicEstimateReady),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
	)
	require.NoError(t, err)
	t.Cleanup(verifier.Close)

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		fetches := verifier.PollFetches(ctx)
		cancel()

		var found *avro.EstimateReady
		fetches.EachRecord(func(r *kgo.Record) {
			var er avro.EstimateReady
			if err := serde.Decode(r.Value, &er); err == nil && er.VoyageID == id {
				found = &er
			}
		})
		if found != nil {
			return *found
		}
	}
	t.Fatalf("estimate.ready for %s not seen within %s", id, timeout)
	return avro.EstimateReady{}
}
