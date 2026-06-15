// Command migrate applies the voyage service's database migrations and exits.
// It shares the embed.FS and Migrate path that app.Run uses (one DDL source for
// dev, compose, L2, and L4). DSN comes from VOYAGE_DB_DSN.
package main

import (
	"context"
	"log"

	"maritime-test-lab/internal/env"
	"maritime-test-lab/services/voyage/adapters/postgres"
)

func main() {
	dsn := env.Or("VOYAGE_DB_DSN", "postgres://postgres:dev@localhost:15432/postgres?sslmode=disable")

	if err := postgres.Migrate(context.Background(), dsn); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	log.Println("migrate: database is up to date")
}
