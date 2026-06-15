//go:build integration

package postgres_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"maritime-test-lab/internal/testfix"
	voyagepg "maritime-test-lab/services/voyage/adapters/postgres"
	"maritime-test-lab/services/voyage/domain"
)

var (
	testDSN  string
	testPool *pgxpool.Pool
)

func TestMain(m *testing.M) {
	ctx := context.Background()

	dsn, stop, err := testfix.StartPostgres(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "testfix:", err)
		os.Exit(1)
	}
	pool, err := voyagepg.Connect(ctx, dsn)
	if err != nil {
		stop()
		fmt.Fprintln(os.Stderr, "connect:", err)
		os.Exit(1)
	}
	testDSN, testPool = dsn, pool

	code := m.Run()
	pool.Close()
	stop()
	os.Exit(code)
}

// stubEncoder stands in for the real Avro/SR encoder: the repository test cares
// that an outbox row is written transactionally, not about its wire format.
func stubEncoder(v domain.Voyage) (string, []byte, error) {
	return "voyage.created", []byte("evt-" + v.ClientRequestID), nil
}

func newRepo() *voyagepg.Repository {
	return voyagepg.NewRepository(testPool, stubEncoder)
}

func outboxCount(t *testing.T, clientRequestID string) int {
	t.Helper()
	var n int
	require.NoError(t, testPool.QueryRow(context.Background(),
		"select count(*) from outbox where payload = $1", []byte("evt-"+clientRequestID)).Scan(&n))
	return n
}

func TestRepository(t *testing.T) {
	repo := newRepo()
	ctx := context.Background()

	t.Run("creates a voyage at version 1, writes one outbox row, reads it back", func(t *testing.T) {
		v := domain.Voyage{ClientRequestID: uuid.NewString(), Origin: "NLRTM", Dest: "SGSIN", DistanceNm: 8200, FeesMinor: 5000}

		created, err := repo.Create(ctx, v)
		require.NoError(t, err)
		assert.Equal(t, int64(1), created.Version)
		assert.Zero(t, created.EstimateMinor, "estimate is pending until the async path applies it")
		assert.Equal(t, 1, outboxCount(t, v.ClientRequestID), "exactly one voyage.created event")

		got, err := repo.Get(ctx, v.ClientRequestID)
		require.NoError(t, err)
		assert.Equal(t, created, got)
	})

	t.Run("create is idempotent and emits no second event", func(t *testing.T) {
		id := uuid.NewString()
		v := domain.Voyage{ClientRequestID: id, Origin: "NLRTM", Dest: "SGSIN", DistanceNm: 8200, FeesMinor: 5000}

		first, err := repo.Create(ctx, v)
		require.NoError(t, err)

		dup := v
		dup.FeesMinor = 99999 // a retry with a different payload must not overwrite
		second, err := repo.Create(ctx, dup)
		require.NoError(t, err)

		assert.Equal(t, first, second, "duplicate create returns the existing voyage unchanged")
		assert.Equal(t, 1, outboxCount(t, id), "duplicate create emits no second event")
	})

	t.Run("get unknown id returns ErrVoyageNotFound", func(t *testing.T) {
		_, err := repo.Get(ctx, "does-not-exist")
		require.ErrorIs(t, err, domain.ErrVoyageNotFound)
	})
}

// TestUpsertEstimate pins the versioned idempotent upsert at the count level: the
// estimate applies exactly once, a redelivery of the identical event is a true
// no-op (applied=false — not a second silent re-apply), and an event for a
// non-matching version or unknown voyage changes nothing.
func TestUpsertEstimate(t *testing.T) {
	repo := newRepo()
	ctx := context.Background()

	id := uuid.NewString()
	_, err := repo.Create(ctx, domain.Voyage{ClientRequestID: id, Origin: "NLRTM", Dest: "SGSIN", DistanceNm: 8200, FeesMinor: 5000})
	require.NoError(t, err)

	t.Run("applies the matching-version estimate once", func(t *testing.T) {
		applied, _, err := repo.UpsertEstimate(ctx, id, 1, 25000)
		require.NoError(t, err)
		assert.True(t, applied, "first delivery applies")

		got, err := repo.Get(ctx, id)
		require.NoError(t, err)
		assert.Equal(t, int64(25000), got.EstimateMinor)
	})

	t.Run("a redelivery of the same estimate is a genuine no-op", func(t *testing.T) {
		applied, _, err := repo.UpsertEstimate(ctx, id, 1, 25000)
		require.NoError(t, err)
		assert.False(t, applied, "at-least-once redelivery must not re-apply — count(*)=1 idempotency")
	})

	t.Run("an estimate for a non-matching version is ignored", func(t *testing.T) {
		applied, _, err := repo.UpsertEstimate(ctx, id, 99, 77777)
		require.NoError(t, err)
		assert.False(t, applied)

		got, err := repo.Get(ctx, id)
		require.NoError(t, err)
		assert.Equal(t, int64(25000), got.EstimateMinor, "the wrong-version estimate never landed")
	})

	t.Run("an estimate for an unknown voyage is a benign no-op", func(t *testing.T) {
		applied, _, err := repo.UpsertEstimate(ctx, "ghost", 1, 100)
		require.NoError(t, err)
		assert.False(t, applied)
	})
}

// TestUpdateHigherVersionWins pins the SQL stale-write guard (D-016) at the cheap
// integration layer instead of only through the full-stack resilience suite: a
// strictly-higher version lands; an equal- or lower-version write is a no-op. The
// equal-version case is what proves the guard is `version < $6`, not `<= $6`.
func TestUpdateHigherVersionWins(t *testing.T) {
	repo := newRepo()
	ctx := context.Background()

	id := uuid.NewString()
	_, err := repo.Create(ctx, domain.Voyage{ClientRequestID: id, Origin: "NLRTM", Dest: "SGSIN", DistanceNm: 8200, FeesMinor: 5000})
	require.NoError(t, err)

	update := func(version, fees int64) domain.Voyage {
		return domain.Voyage{ClientRequestID: id, Origin: "NLRTM", Dest: "SGSIN", DistanceNm: 8200, FeesMinor: fees, Version: version}
	}

	t.Run("a strictly-higher version lands", func(t *testing.T) {
		got, err := repo.Update(ctx, update(2, 7000))
		require.NoError(t, err)
		assert.Equal(t, int64(2), got.Version)
		assert.Equal(t, int64(7000), got.FeesMinor)
	})

	t.Run("an equal-version write is a no-op", func(t *testing.T) {
		got, err := repo.Update(ctx, update(2, 9999))
		require.NoError(t, err)
		assert.Equal(t, int64(2), got.Version, "version unchanged")
		assert.Equal(t, int64(7000), got.FeesMinor, "a tie must not overwrite the stored edit")
	})

	t.Run("a lower-version write is a no-op", func(t *testing.T) {
		got, err := repo.Update(ctx, update(1, 1))
		require.NoError(t, err)
		assert.Equal(t, int64(2), got.Version)
		assert.Equal(t, int64(7000), got.FeesMinor, "a stale write must not overwrite")
	})
}

// TestConcurrentCreateDedupes drives the race the sequential idempotency test
// can't: N goroutines create the SAME client_request_id at once. The insert-or-
// return path must leave exactly one row (the unique index is the arbiter), and
// every caller must observe the same voyage at version 1 — no duplicate, no lost
// winner, no ErrVoyageNotFound from a caller that lost the race.
func TestConcurrentCreateDedupes(t *testing.T) {
	repo := newRepo()
	ctx := context.Background()

	id := uuid.NewString()
	const racers = 16

	var wg sync.WaitGroup
	results := make([]domain.Voyage, racers)
	errs := make([]error, racers)
	wg.Add(racers)
	for i := 0; i < racers; i++ {
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = repo.Create(ctx, domain.Voyage{
				ClientRequestID: id, Origin: "NLRTM", Dest: "SGSIN", DistanceNm: 8200, FeesMinor: 5000,
			})
		}(i)
	}
	wg.Wait()

	for i := 0; i < racers; i++ {
		require.NoErrorf(t, errs[i], "racer %d", i)
		assert.Equal(t, id, results[i].ClientRequestID)
		assert.Equal(t, int64(1), results[i].Version, "every racer sees version 1")
	}

	var rows int
	require.NoError(t, testPool.QueryRow(ctx,
		"select count(*) from voyages where client_request_id = $1", id).Scan(&rows))
	assert.Equal(t, 1, rows, "exactly one row survives the race")
	assert.Equal(t, 1, outboxCount(t, id), "exactly one voyage.created event, never a duplicate")
}
