// Command voyage runs the voyage service. It is deliberately thin: read config,
// validate, run with a signal-cancelled context. All logic lives in app/ and
// the adapters it wires.
package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"maritime-test-lab/services/voyage/app"
)

func main() {
	cfg := app.LoadConfig()
	if err := cfg.Validate(); err != nil {
		log.Fatalf("voyage: invalid config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx, cfg); err != nil {
		log.Fatalf("voyage: %v", err)
	}
}
