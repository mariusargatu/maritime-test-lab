package app

import (
	"fmt"

	"maritime-test-lab/internal/env"
)

// Config is the voyage service's runtime configuration. main reads it from the
// environment and Validate rejects bad values before anything starts (fail-fast).
type Config struct {
	GRPCAddr          string   // host:port for the gRPC listener
	MetricsAddr       string   // host:port for the Prometheus /metrics HTTP server
	DBDSN             string   // Postgres DSN
	EstimatorAddr     string   // host:port of the estimator gRPC service (sync quote)
	KafkaBrokers      []string // Kafka seed brokers
	SchemaRegistryURL string   // Schema Registry base URL
	OTLPEndpoint      string   // OTLP/gRPC trace target; empty disables tracing
}

// LoadConfig reads configuration from the environment, applying defaults.
func LoadConfig() Config {
	return Config{
		GRPCAddr:          env.Or("VOYAGE_GRPC_ADDR", ":9000"),
		MetricsAddr:       env.Or("VOYAGE_METRICS_ADDR", ":9100"),
		DBDSN:             env.Or("VOYAGE_DB_DSN", "postgres://postgres:dev@localhost:15432/postgres?sslmode=disable"),
		EstimatorAddr:     env.Or("ESTIMATOR_ADDR", "localhost:9001"),
		KafkaBrokers:      env.List("KAFKA_BROKERS", "localhost:19092"),
		SchemaRegistryURL: env.Or("SCHEMA_REGISTRY_URL", "http://localhost:18081"),
		OTLPEndpoint:      env.Or("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
	}
}

// Validate returns an error describing the first invalid field, or nil.
func (c Config) Validate() error {
	switch {
	case c.GRPCAddr == "":
		return fmt.Errorf("config: VOYAGE_GRPC_ADDR must not be empty")
	case c.MetricsAddr == "":
		return fmt.Errorf("config: VOYAGE_METRICS_ADDR must not be empty")
	case c.DBDSN == "":
		return fmt.Errorf("config: VOYAGE_DB_DSN must not be empty")
	case c.EstimatorAddr == "":
		return fmt.Errorf("config: ESTIMATOR_ADDR must not be empty")
	case len(c.KafkaBrokers) == 0:
		return fmt.Errorf("config: KAFKA_BROKERS must not be empty")
	case c.SchemaRegistryURL == "":
		return fmt.Errorf("config: SCHEMA_REGISTRY_URL must not be empty")
	default:
		return nil
	}
}
