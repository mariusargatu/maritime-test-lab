// Package app wires the estimator service: an Avro/SR serde, a Kafka group
// consumer of voyage.created (autocommit off), and the sync Estimate gRPC server.
// Stateless — no DB, no outbox.
package app

import (
	"context"
	"fmt"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/plugin/kotel"
	"google.golang.org/grpc"

	estimatorv1 "maritime-test-lab/gen/proto/estimator/v1"
	"maritime-test-lab/internal/avroserde"
	"maritime-test-lab/internal/eventreg"
	"maritime-test-lab/internal/money"
	"maritime-test-lab/internal/serve"
	"maritime-test-lab/internal/tracing"
	"maritime-test-lab/schemas"
	"maritime-test-lab/services/estimator/adapters/grpcserver"
	estimatorkafka "maritime-test-lab/services/estimator/adapters/kafka"
)

// Run starts the estimator service and blocks until ctx is cancelled.
func Run(ctx context.Context, cfg Config) error {
	shutdownTracing, err := tracing.Init(ctx, "estimator", cfg.OTLPEndpoint)
	if err != nil {
		return fmt.Errorf("estimator app: %w", err)
	}
	defer func() { _ = shutdownTracing(context.Background()) }()

	serde, err := avroserde.New(ctx, cfg.SchemaRegistryURL, eventreg.All()...)
	if err != nil {
		return fmt.Errorf("estimator app: %w", err)
	}

	client, err := kgo.NewClient(
		kgo.SeedBrokers(cfg.KafkaBrokers...),
		kgo.WithHooks(kotel.NewKotel(kotel.WithTracer(kotel.NewTracer())).Hooks()...),
		kgo.ConsumeTopics(schemas.TopicVoyageCreated),
		kgo.ConsumerGroup(estimatorkafka.ConsumerGroup),
		kgo.DisableAutoCommit(),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
	)
	if err != nil {
		return fmt.Errorf("estimator app: kafka client: %w", err)
	}
	defer client.Close()

	rate := money.FromUSD(cfg.RatePerNmMinor)
	go estimatorkafka.NewConsumer(client, serde, rate, time.Now).Run(ctx)

	return serve.GRPC(ctx, cfg.GRPCAddr, func(server *grpc.Server) {
		estimatorv1.RegisterEstimatorServiceServer(server, grpcserver.New(rate))
	})
}
