package domain

import (
	"context"
	"time"
)

// Store is the durable disk queue: pending operations survive process restarts
// (the offline-first guarantee). Implemented by adapters/jsonlstore.
type Store interface {
	// Append upserts an operation by ClientRequestID and persists it durably.
	Append(op Operation) error
	// Pending returns every operation not yet acked.
	Pending() ([]Operation, error)
	// Ack removes an operation after the server has durably accepted it.
	Ack(clientRequestID string) error
}

// Transport sends an operation to the server, returning nil only on a durable
// ack. Implemented by adapters/graphqltransport. Every send carries a context
// deadline — a timeout IS how the client detects it is offline (D-018).
type Transport interface {
	Send(ctx context.Context, op Operation) error
}

// Clock is the injected time source; tests pass a deterministic fake.
type Clock interface {
	Now() time.Time
}
