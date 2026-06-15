package app

import (
	"fmt"
	"strconv"

	"maritime-test-lab/internal/env"
)

// Config is the estimator service's runtime configuration. Per §13 the estimator
// listens on :9001. The per-nm rate is typed config (D-038), validated > 0.
type Config struct {
	GRPCAddr          string
	KafkaBrokers      []string
	SchemaRegistryURL string
	RatePerNmMinor    int64
	OTLPEndpoint      string // OTLP/gRPC trace target; empty disables tracing
}

// LoadConfig reads configuration from the environment, applying defaults. It
// returns an error if RATE_PER_NM_MINOR is not an integer.
func LoadConfig() (Config, error) {
	rate, err := strconv.ParseInt(env.Or("RATE_PER_NM_MINOR", "100"), 10, 64)
	if err != nil {
		return Config{}, fmt.Errorf("config: RATE_PER_NM_MINOR: %w", err)
	}
	return Config{
		GRPCAddr:          env.Or("ESTIMATOR_GRPC_ADDR", ":9001"),
		KafkaBrokers:      env.List("KAFKA_BROKERS", "localhost:19092"),
		SchemaRegistryURL: env.Or("SCHEMA_REGISTRY_URL", "http://localhost:18081"),
		RatePerNmMinor:    rate,
		OTLPEndpoint:      env.Or("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
	}, nil
}

// Validate returns an error describing the first invalid field, or nil.
func (c Config) Validate() error {
	switch {
	case c.GRPCAddr == "":
		return fmt.Errorf("config: ESTIMATOR_GRPC_ADDR must not be empty")
	case len(c.KafkaBrokers) == 0:
		return fmt.Errorf("config: KAFKA_BROKERS must not be empty")
	case c.SchemaRegistryURL == "":
		return fmt.Errorf("config: SCHEMA_REGISTRY_URL must not be empty")
	case c.RatePerNmMinor <= 0:
		return fmt.Errorf("config: RATE_PER_NM_MINOR must be positive, got %d", c.RatePerNmMinor)
	default:
		return nil
	}
}
