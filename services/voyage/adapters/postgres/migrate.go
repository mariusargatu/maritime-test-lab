// Package postgres is the voyage service's outbound persistence adapter. Phase 0
// ships only the migration path; the VoyageRepository implementation lands in
// Phase 2. The embedded migrations are the single DDL source — cmd/migrate,
// app.Run, the L2 testcontainer, and the L4 compose stack all apply these same
// files, so dev and CI can never drift.
package postgres

import (
	"context"
	"database/sql"
	"embed"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Migrate applies all pending migrations to the database at dsn, then returns.
// It is safe to run repeatedly: already-applied migrations are skipped, so a
// re-run is a no-op.
func Migrate(ctx context.Context, dsn string) (err error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("migrate: open db: %w", err)
	}
	defer func() {
		if cerr := db.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("migrate: close db: %w", cerr)
		}
	}()

	goose.SetBaseFS(migrations)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("migrate: set dialect: %w", err)
	}
	if err := goose.UpContext(ctx, db, "migrations"); err != nil {
		return fmt.Errorf("migrate: up: %w", err)
	}
	return nil
}
