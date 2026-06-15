package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	"maritime-test-lab/services/voyage/domain"
)

// OutboxEncoder turns a created voyage into the topic and serialized payload of
// its voyage.created event. Provided by the wiring (it owns the Avro/SR serde),
// so the repository stays free of wire-format concerns.
type OutboxEncoder func(domain.Voyage) (topic string, payload []byte, err error)

// Repository implements domain.VoyageRepository over Postgres (pgx/v5).
type Repository struct {
	pool   *pgxpool.Pool
	encode OutboxEncoder
}

// NewRepository wraps a pgx pool. encode is used to write the voyage.created
// event into the transactional outbox in the same transaction as the insert.
func NewRepository(pool *pgxpool.Pool, encode OutboxEncoder) *Repository {
	return &Repository{pool: pool, encode: encode}
}

const insertVoyage = `
INSERT INTO voyages (client_request_id, origin, dest, distance_nm, fees_minor, version)
VALUES ($1, $2, $3, $4, $5, 1)
ON CONFLICT (client_request_id) DO NOTHING
RETURNING client_request_id, origin, dest, distance_nm, fees_minor, version, estimate_minor`

const insertOutbox = `INSERT INTO outbox (topic, payload, trace_carrier) VALUES ($1, $2, $3)`

const selectVoyage = `
SELECT client_request_id, origin, dest, distance_nm, fees_minor, version, estimate_minor
FROM voyages
WHERE client_request_id = $1`

// Versioned idempotent upsert: the estimate applies only to the voyage at the
// exact version it was priced for. An event for a non-matching version is a no-op.
// The `IS DISTINCT FROM` guard makes a redelivery of the SAME estimate a genuine
// no-op (no row returned → applied=false), so at-least-once delivery costs exactly
// one apply, not one-per-copy — the identity-scoped count(*)=1 idempotency rule
// (CLAUDE.md). Order-independent. RETURNING created_at measures end-to-end latency.
const upsertEstimate = `
UPDATE voyages SET estimate_minor = $3
WHERE client_request_id = $1 AND version = $2 AND estimate_minor IS DISTINCT FROM $3
RETURNING created_at`

// Create inserts the voyage and its voyage.created outbox row in one transaction
// (the transactional outbox: the event can never be lost relative to the write).
// It is idempotent: a duplicate client_request_id returns the existing voyage and
// emits no second event. New voyages start at version 1.
func (r *Repository) Create(ctx context.Context, v domain.Voyage) (domain.Voyage, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Voyage{}, fmt.Errorf("create voyage %s: begin: %w", v.ClientRequestID, err)
	}
	defer func() {
		// No-op once Commit has run; ignore the resulting ErrTxClosed.
		_ = tx.Rollback(ctx)
	}()

	created, err := scanVoyage(tx.QueryRow(ctx, insertVoyage, v.ClientRequestID, v.Origin, v.Dest, v.DistanceNm, v.FeesMinor))
	if errors.Is(err, pgx.ErrNoRows) {
		// ON CONFLICT DO NOTHING returned no row: the voyage already exists. Release
		// this (empty) transaction's connection BEFORE the follow-up read, or under
		// concurrent same-key creates every goroutine would hold its tx connection
		// while reaching for a second one for Get — a pool-exhaustion deadlock.
		_ = tx.Rollback(ctx)
		dedupeTotal.Inc()
		return r.Get(ctx, v.ClientRequestID)
	}
	if err != nil {
		return domain.Voyage{}, fmt.Errorf("create voyage %s: %w", v.ClientRequestID, err)
	}

	topic, payload, err := r.encode(created)
	if err != nil {
		return domain.Voyage{}, fmt.Errorf("create voyage %s: encode event: %w", v.ClientRequestID, err)
	}

	traceJSON, err := injectTraceCarrier(ctx)
	if err != nil {
		return domain.Voyage{}, fmt.Errorf("create voyage %s: %w", v.ClientRequestID, err)
	}

	if _, err := tx.Exec(ctx, insertOutbox, topic, payload, traceJSON); err != nil {
		return domain.Voyage{}, fmt.Errorf("create voyage %s: outbox: %w", v.ClientRequestID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Voyage{}, fmt.Errorf("create voyage %s: commit: %w", v.ClientRequestID, err)
	}
	return created, nil
}

// injectTraceCarrier serializes the current request's trace context as a W3C
// carrier so the outbox poller can publish the event under it — keeping one
// trace across the async outbox boundary.
func injectTraceCarrier(ctx context.Context) ([]byte, error) {
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	traceJSON, err := json.Marshal(carrier)
	if err != nil {
		return nil, fmt.Errorf("trace carrier: %w", err)
	}
	return traceJSON, nil
}

const updateVoyageHigherWins = `
UPDATE voyages SET origin = $2, dest = $3, distance_nm = $4, fees_minor = $5, version = $6
WHERE client_request_id = $1 AND version < $6`

// Update applies the higher-version-wins merge rule (D-016): the write lands only
// if its version is strictly higher than the stored one; a stale write is a
// no-op. It returns the current persisted state either way.
func (r *Repository) Update(ctx context.Context, v domain.Voyage) (domain.Voyage, error) {
	if _, err := r.pool.Exec(ctx, updateVoyageHigherWins, v.ClientRequestID, v.Origin, v.Dest, v.DistanceNm, v.FeesMinor, v.Version); err != nil {
		return domain.Voyage{}, fmt.Errorf("update voyage %s: %w", v.ClientRequestID, err)
	}
	return r.Get(ctx, v.ClientRequestID)
}

// Get returns the voyage by client_request_id, or domain.ErrVoyageNotFound.
func (r *Repository) Get(ctx context.Context, clientRequestID string) (domain.Voyage, error) {
	v, err := scanVoyage(r.pool.QueryRow(ctx, selectVoyage, clientRequestID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Voyage{}, fmt.Errorf("get voyage %s: %w", clientRequestID, domain.ErrVoyageNotFound)
	}
	if err != nil {
		return domain.Voyage{}, fmt.Errorf("get voyage %s: %w", clientRequestID, err)
	}
	return v, nil
}

// UpsertEstimate applies an estimate.ready event with the versioned idempotent
// upsert. It reports whether a row was updated (false = no-op: unknown voyage or
// non-matching version) and, when applied, the voyage's created_at so the caller
// can record end-to-end latency.
func (r *Repository) UpsertEstimate(ctx context.Context, voyageID string, voyageVersion, amountMinor int64) (bool, time.Time, error) {
	var createdAt time.Time
	err := r.pool.QueryRow(ctx, upsertEstimate, voyageID, voyageVersion, amountMinor).Scan(&createdAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, time.Time{}, nil // benign no-op
	}
	if err != nil {
		return false, time.Time{}, fmt.Errorf("upsert estimate %s v%d: %w", voyageID, voyageVersion, err)
	}
	return true, createdAt, nil
}

func scanVoyage(row pgx.Row) (domain.Voyage, error) {
	var (
		v        domain.Voyage
		estimate *int64 // estimate_minor is nullable; NULL means pending
	)
	if err := row.Scan(&v.ClientRequestID, &v.Origin, &v.Dest, &v.DistanceNm, &v.FeesMinor, &v.Version, &estimate); err != nil {
		return domain.Voyage{}, err
	}
	if estimate != nil {
		v.EstimateMinor = *estimate
	}
	return v, nil
}
