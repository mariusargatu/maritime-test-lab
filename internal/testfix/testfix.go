// Package testfix builds the L2 service-integration fixtures: throwaway
// containers wired the way the running services expect. Each starter returns the
// connection detail, a stop func, and an error — no dependency on testing — so a
// package's TestMain can boot ONE shared fixture and tear it down once. Imported
// only by integration-tagged tests; `make test-svc` runs them with -p 1 so one
// container set is alive at a time.
package testfix

import (
	"context"
	"errors"
	"fmt"

	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcredpanda "github.com/testcontainers/testcontainers-go/modules/redpanda"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"

	"maritime-test-lab/schemas"
	voyagepg "maritime-test-lab/services/voyage/adapters/postgres"
)

// StartPostgres runs a throwaway Postgres and applies the voyage migrations via
// the same goose embed.FS path production uses, so the schema can never drift
// from dev. Returns the DSN and a stop func.
func StartPostgres(ctx context.Context) (dsn string, stop func(), err error) {
	pg, err := tcpostgres.Run(ctx, "postgres:16.6",
		tcpostgres.WithDatabase("voyage"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("dev"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		return "", nil, fmt.Errorf("testfix postgres: %w", err)
	}
	stop = func() { _ = pg.Terminate(context.Background()) }

	dsn, err = pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		stop()
		return "", nil, fmt.Errorf("testfix postgres dsn: %w", err)
	}
	if err := voyagepg.Migrate(ctx, dsn); err != nil {
		stop()
		return "", nil, fmt.Errorf("testfix migrate: %w", err)
	}
	return dsn, stop, nil
}

// StartRedpanda runs a throwaway Redpanda with its built-in Schema Registry.
// Returns the Kafka broker address, the Schema Registry URL, and a stop func.
func StartRedpanda(ctx context.Context) (brokers, schemaRegistryURL string, stop func(), err error) {
	rp, err := tcredpanda.Run(ctx, "redpandadata/redpanda:v25.1.1")
	if err != nil {
		return "", "", nil, fmt.Errorf("testfix redpanda: %w", err)
	}
	stop = func() { _ = rp.Terminate(context.Background()) }

	brokers, err = rp.KafkaSeedBroker(ctx)
	if err != nil {
		stop()
		return "", "", nil, fmt.Errorf("testfix redpanda broker: %w", err)
	}
	schemaRegistryURL, err = rp.SchemaRegistryAddress(ctx)
	if err != nil {
		stop()
		return "", "", nil, fmt.Errorf("testfix redpanda schema registry: %w", err)
	}

	// Mirror compose's topic-init: create the four topics explicitly (franz-go
	// does not request auto-creation by default).
	if err := createTopics(ctx, brokers,
		schemas.TopicVoyageCreated, schemas.TopicEstimateReady,
		schemas.TopicVoyageCreatedDLQ, schemas.TopicEstimateReadyDLQ); err != nil {
		stop()
		return "", "", nil, fmt.Errorf("testfix redpanda topics: %w", err)
	}
	return brokers, schemaRegistryURL, stop, nil
}

func createTopics(ctx context.Context, brokers string, topics ...string) error {
	cl, err := kgo.NewClient(kgo.SeedBrokers(brokers))
	if err != nil {
		return err
	}
	defer cl.Close()

	resp, err := kadm.NewClient(cl).CreateTopics(ctx, 1, 1, nil, topics...)
	if err != nil {
		return err
	}
	for _, ct := range resp {
		if ct.Err != nil && !errors.Is(ct.Err, kerr.TopicAlreadyExists) {
			return fmt.Errorf("create topic %s: %w", ct.Topic, ct.Err)
		}
	}
	return nil
}
