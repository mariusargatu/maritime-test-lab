package domain

import (
	"context"
	"errors"

	"maritime-test-lab/internal/money"
)

// ErrEstimatorUnavailable signals the estimator could not be reached. Adapters
// translate infra errors (e.g. gRPC codes.Unavailable) into this so the domain
// can degrade gracefully without importing grpc.
var ErrEstimatorUnavailable = errors.New("voyage: estimator unavailable")

// The ports the voyage domain needs. Kept small (1-3 methods) and owned by the
// consumer — adapters implement them, tests pass fakes.

// VoyageRepository persists and retrieves voyages. Implemented by
// adapters/postgres (Phase 2).
type VoyageRepository interface {
	// Create inserts the voyage, or returns the existing one if its
	// client_request_id is already present — idempotent, never a duplicate.
	Create(ctx context.Context, v Voyage) (Voyage, error)
	// Update applies the higher-version-wins merge rule (D-016): the write lands
	// only if its version is strictly higher; a stale write is a no-op. Returns
	// the current persisted state either way.
	Update(ctx context.Context, v Voyage) (Voyage, error)
	Get(ctx context.Context, clientRequestID string) (Voyage, error)
}

// EventPublisher is the consumer-owned port for emitting voyage events. NOTE:
// the production create path publishes via the Postgres transactional outbox
// (repository.Create → kafka.Poller), not through this port (D-005); it is kept
// as the canonical small-port example exercised by the domain test's fake.
type EventPublisher interface {
	PublishVoyageCreated(ctx context.Context, v Voyage) error
}

// Estimator returns a provisional cost quote for a voyage. Implemented by the
// estimator gRPC client adapter (adapters/estimatorclient).
type Estimator interface {
	Estimate(ctx context.Context, v Voyage) (money.Money, error)
}
