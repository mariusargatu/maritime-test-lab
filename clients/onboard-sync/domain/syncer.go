package domain

import (
	"context"
	"fmt"
	"time"
)

// SyncStatus is the observable state of the queue — what the resilience tests
// assert on instead of sleeping (D-017).
type SyncStatus struct {
	QueueDepth  int
	RetryCount  int
	LastAttempt time.Time
	LastError   string
}

// Syncer drives operations from the disk queue to the server. It is the use-case
// that wires the ports; it holds retry counters (a coordinator, not a value).
type Syncer struct {
	store     Store
	transport Transport
	clock     Clock
	retry     RetryPolicy

	retryCount  int
	lastAttempt time.Time
	lastError   string
}

// NewSyncer constructs a Syncer from its ports and retry policy.
func NewSyncer(store Store, transport Transport, clock Clock, retry RetryPolicy) *Syncer {
	return &Syncer{store: store, transport: transport, clock: clock, retry: retry}
}

// Enqueue stages an operation, coalescing with any pending edit for the same
// voyage via the higher-version-wins rule, then persists it durably.
func (s *Syncer) Enqueue(op Operation) error {
	pending, err := s.store.Pending()
	if err != nil {
		return fmt.Errorf("enqueue %s: %w", op.ClientRequestID, err)
	}
	for _, existing := range pending {
		if existing.ClientRequestID == op.ClientRequestID {
			op = HigherVersionWins(existing, op)
			break
		}
	}
	if err := s.store.Append(op); err != nil {
		return fmt.Errorf("enqueue %s: %w", op.ClientRequestID, err)
	}
	return nil
}

// SyncOnce attempts every pending operation once. Acked operations are removed;
// a failed send (e.g. an offline deadline) leaves the operation queued for the
// next attempt and bumps the retry count. It never sleeps — the caller decides
// when to retry, after RetryBackoff. Returns the number successfully synced.
func (s *Syncer) SyncOnce(ctx context.Context) (int, error) {
	pending, err := s.store.Pending()
	if err != nil {
		return 0, fmt.Errorf("sync: %w", err)
	}

	synced := 0
	for _, op := range pending {
		s.lastAttempt = s.clock.Now()
		if err := s.transport.Send(ctx, op); err != nil {
			s.retryCount++
			s.lastError = err.Error()
			continue
		}
		if err := s.store.Ack(op.ClientRequestID); err != nil {
			return synced, fmt.Errorf("sync ack %s: %w", op.ClientRequestID, err)
		}
		synced++
	}
	return synced, nil
}

// RetryBackoff returns the wait before the next attempt given the failures so far.
func (s *Syncer) RetryBackoff() time.Duration {
	return s.retry.Backoff(s.retryCount)
}

// SyncStatus reports the current queue state.
func (s *Syncer) SyncStatus() (SyncStatus, error) {
	pending, err := s.store.Pending()
	if err != nil {
		return SyncStatus{}, fmt.Errorf("sync status: %w", err)
	}
	return SyncStatus{
		QueueDepth:  len(pending),
		RetryCount:  s.retryCount,
		LastAttempt: s.lastAttempt,
		LastError:   s.lastError,
	}, nil
}
