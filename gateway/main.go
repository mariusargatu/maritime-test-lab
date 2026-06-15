// Command gateway runs the GraphQL gateway. Thin by design: read config,
// validate, run with a signal-cancelled context.
package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"maritime-test-lab/gateway/app"
)

func main() {
	cfg := app.LoadConfig()
	if err := cfg.Validate(); err != nil {
		log.Fatalf("gateway: invalid config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx, cfg); err != nil {
		log.Fatalf("gateway: %v", err)
	}
}
