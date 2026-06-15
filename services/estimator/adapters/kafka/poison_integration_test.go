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

// A poison pill (undecodable voyage.created) costs exactly one DLQ entry and does
// not stall the consumer: a valid event right after it is still priced.
func TestEstimatorConsumerPoisonPill(t *testing.T) {
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

	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go estimatorkafka.NewConsumer(consumerClient, serde, money.FromUSD(200), time.Now).Run(cctx)

	producer, err := kgo.NewClient(kgo.SeedBrokers(brokers))
	require.NoError(t, err)
	t.Cleanup(producer.Close)

	// garbage first, then a valid event
	require.NoError(t, producer.ProduceSync(ctx, &kgo.Record{Topic: schemas.TopicVoyageCreated, Value: []byte("not avro")}).FirstErr())
	id := uuid.NewString()
	valid, err := serde.Encode(avro.VoyageCreated{ClientRequestID: id, Origin: "NLRTM", Dest: "SGSIN", DistanceNm: 100, FeesMinor: 5000, Version: 1})
	require.NoError(t, err)
	require.NoError(t, producer.ProduceSync(ctx, &kgo.Record{Topic: schemas.TopicVoyageCreated, Value: valid}).FirstErr())

	// the valid event is still priced past the poison pill
	got := pollEstimateReady(t, brokers, serde, id, 25*time.Second)
	assert.Equal(t, int64(25000), got.AmountMinor)

	// exactly one DLQ entry (DLQ depth is the documented identity-scoping exception)
	assert.Equal(t, 1, dlqDepth(t, brokers, schemas.TopicVoyageCreatedDLQ, 5*time.Second))
}

func dlqDepth(t *testing.T, brokers, topic string, window time.Duration) int {
	t.Helper()
	cl, err := kgo.NewClient(
		kgo.SeedBrokers(brokers),
		kgo.ConsumeTopics(topic),
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
		fetches.EachRecord(func(*kgo.Record) { count++ })
	}
	return count
}
