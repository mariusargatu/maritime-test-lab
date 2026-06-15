// Package app wires the voyage service: migrate, construct the postgres
// repository (with the outbox encoder), the estimator gRPC client, the Avro/SR
// serde, and the Kafka client; start the outbox poller; serve gRPC until the
// context is cancelled. The estimate.ready consumer joins here in the next step.
package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/plugin/kotel"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	estimatorv1 "maritime-test-lab/gen/proto/estimator/v1"
	voyagev1 "maritime-test-lab/gen/proto/voyage/v1"
	"maritime-test-lab/internal/avroserde"
	"maritime-test-lab/internal/eventreg"
	"maritime-test-lab/internal/serve"
	"maritime-test-lab/internal/tracing"
	"maritime-test-lab/schemas"
	"maritime-test-lab/services/voyage/adapters/estimatorclient"
	"maritime-test-lab/services/voyage/adapters/grpcserver"
	"maritime-test-lab/services/voyage/adapters/kafka"
	"maritime-test-lab/services/voyage/adapters/postgres"
)

const outboxPollInterval = 250 * time.Millisecond

// Run starts the voyage service and blocks until ctx is cancelled.
func Run(ctx context.Context, cfg Config) error {
	shutdownTracing, err := tracing.Init(ctx, "voyage", cfg.OTLPEndpoint)
	if err != nil {
		return fmt.Errorf("voyage app: %w", err)
	}
	defer func() { _ = shutdownTracing(context.Background()) }()

	if err := postgres.Migrate(ctx, cfg.DBDSN); err != nil {
		return fmt.Errorf("voyage app: %w", err)
	}

	pool, err := postgres.Connect(ctx, cfg.DBDSN)
	if err != nil {
		return fmt.Errorf("voyage app: %w", err)
	}
	defer pool.Close()

	serde, err := avroserde.New(ctx, cfg.SchemaRegistryURL, eventreg.All()...)
	if err != nil {
		return fmt.Errorf("voyage app: %w", err)
	}

	// kotel propagates W3C tracecontext through Kafka record headers on every
	// produce/consume, so a trace continues across the async boundary.
	kotelHooks := kgo.WithHooks(kotel.NewKotel(kotel.WithTracer(kotel.NewTracer())).Hooks()...)

	producer, err := kgo.NewClient(kgo.SeedBrokers(cfg.KafkaBrokers...), kotelHooks)
	if err != nil {
		return fmt.Errorf("voyage app: kafka producer: %w", err)
	}
	defer producer.Close()

	consumer, err := kgo.NewClient(
		kgo.SeedBrokers(cfg.KafkaBrokers...),
		kotelHooks,
		kgo.ConsumeTopics(schemas.TopicEstimateReady),
		kgo.ConsumerGroup(kafka.EstimateConsumerGroup),
		kgo.DisableAutoCommit(),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
	)
	if err != nil {
		return fmt.Errorf("voyage app: kafka consumer: %w", err)
	}
	defer consumer.Close()

	repo := postgres.NewRepository(pool, kafka.VoyageCreatedEncoder(serde))

	conn, err := grpc.NewClient(cfg.EstimatorAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	)
	if err != nil {
		return fmt.Errorf("voyage app: dial estimator: %w", err)
	}
	defer func() { _ = conn.Close() }()
	estimator := estimatorclient.New(estimatorv1.NewEstimatorServiceClient(conn))

	go kafka.NewPoller(pool, producer, outboxPollInterval).Run(ctx)
	go kafka.NewEstimateConsumer(consumer, serde, repo).Run(ctx)
	go kafka.ReportLag(ctx, producer, kafka.EstimateConsumerGroup, 5*time.Second)
	go serveMetrics(ctx, cfg.MetricsAddr)

	return serve.GRPC(ctx, cfg.GRPCAddr, func(server *grpc.Server) {
		voyagev1.RegisterVoyageServiceServer(server, grpcserver.New(repo, estimator))
	})
}

// serveMetrics runs the Prometheus /metrics HTTP server until ctx is cancelled.
func serveMetrics(ctx context.Context, addr string) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Printf("voyage metrics server: %v", err)
	}
}
