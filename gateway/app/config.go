package app

import (
	"fmt"

	"maritime-test-lab/internal/env"
)

// Config is the gateway's runtime configuration.
type Config struct {
	HTTPAddr     string // host:port for the GraphQL HTTP server
	VoyageAddr   string // host:port of the voyage gRPC service (the only edge)
	OTLPEndpoint string // OTLP/gRPC trace target; empty disables tracing
}

// LoadConfig reads configuration from the environment, applying defaults.
func LoadConfig() Config {
	return Config{
		HTTPAddr:     env.Or("GATEWAY_HTTP_ADDR", ":8080"),
		VoyageAddr:   env.Or("VOYAGE_ADDR", "localhost:9000"),
		OTLPEndpoint: env.Or("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
	}
}

// Validate returns an error describing the first invalid field, or nil.
func (c Config) Validate() error {
	switch {
	case c.HTTPAddr == "":
		return fmt.Errorf("config: GATEWAY_HTTP_ADDR must not be empty")
	case c.VoyageAddr == "":
		return fmt.Errorf("config: VOYAGE_ADDR must not be empty")
	default:
		return nil
	}
}
