// Command estimator runs the estimator service. Thin by design: read config,
// validate, run with a signal-cancelled context.
package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"maritime-test-lab/services/estimator/app"
)

func main() {
	cfg, err := app.LoadConfig()
	if err != nil {
		log.Fatalf("estimator: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		log.Fatalf("estimator: invalid config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx, cfg); err != nil {
		log.Fatalf("estimator: %v", err)
	}
}
