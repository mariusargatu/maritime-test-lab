package domain_test

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"maritime-test-lab/clients/onboard-sync/domain"
)

// Hand-written fakes for the three ports (preferred in domain tests, CLAUDE.md).

type fakeStore struct{ ops map[string]domain.Operation }

func newFakeStore() *fakeStore { return &fakeStore{ops: map[string]domain.Operation{}} }

func (f *fakeStore) Append(op domain.Operation) error {
	f.ops[op.ClientRequestID] = op
	return nil
}

func (f *fakeStore) Pending() ([]domain.Operation, error) {
	out := make([]domain.Operation, 0, len(f.ops))
	for _, op := range f.ops {
		out = append(out, op)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ClientRequestID < out[j].ClientRequestID })
	return out, nil
}

func (f *fakeStore) Ack(id string) error {
	delete(f.ops, id)
	return nil
}

type fakeTransport struct {
	offline bool
	sent    []domain.Operation
}

func (f *fakeTransport) Send(_ context.Context, op domain.Operation) error {
	if f.offline {
		return errors.New("deadline exceeded")
	}
	f.sent = append(f.sent, op)
	return nil
}

type fakeClock struct{ t time.Time }

func (f fakeClock) Now() time.Time { return f.t }

func newSyncer(store domain.Store, tr domain.Transport) *domain.Syncer {
	clock := fakeClock{t: time.Unix(1_700_000_000, 0)}
	return domain.NewSyncer(store, tr, clock, domain.NewRetryPolicy(time.Millisecond, time.Second, 1))
}

func TestSyncerDrainsQueueOnAck(t *testing.T) {
	store := newFakeStore()
	tr := &fakeTransport{}
	s := newSyncer(store, tr)

	require.NoError(t, s.Enqueue(domain.Operation{ClientRequestID: "a", Kind: domain.KindCreate, Version: 1}))

	before, err := s.SyncStatus()
	require.NoError(t, err)
	assert.Equal(t, 1, before.QueueDepth)

	synced, err := s.SyncOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, synced)
	assert.Len(t, tr.sent, 1)

	after, err := s.SyncStatus()
	require.NoError(t, err)
	assert.Equal(t, 0, after.QueueDepth)
}

func TestSyncerKeepsQueuedWhileOffline(t *testing.T) {
	store := newFakeStore()
	tr := &fakeTransport{offline: true}
	s := newSyncer(store, tr)

	require.NoError(t, s.Enqueue(domain.Operation{ClientRequestID: "a", Kind: domain.KindCreate, Version: 1}))

	synced, err := s.SyncOnce(context.Background())
	require.NoError(t, err)
	assert.Zero(t, synced)

	offline, _ := s.SyncStatus()
	assert.Equal(t, 1, offline.QueueDepth, "operation stays queued while offline")
	assert.Equal(t, 1, offline.RetryCount)
	assert.NotEmpty(t, offline.LastError)

	tr.offline = false // the vessel reconnects
	synced, err = s.SyncOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, synced)

	healed, _ := s.SyncStatus()
	assert.Zero(t, healed.QueueDepth)
}

func TestSyncerCoalescesToHigherVersion(t *testing.T) {
	store := newFakeStore()
	s := newSyncer(store, &fakeTransport{offline: true})

	require.NoError(t, s.Enqueue(domain.Operation{ClientRequestID: "a", Version: 1, FeesMinor: 100}))
	require.NoError(t, s.Enqueue(domain.Operation{ClientRequestID: "a", Version: 2, FeesMinor: 200}))
	require.NoError(t, s.Enqueue(domain.Operation{ClientRequestID: "a", Version: 1, FeesMinor: 999})) // stale

	pending, err := store.Pending()
	require.NoError(t, err)
	require.Len(t, pending, 1, "same voyage coalesces to one queued op")
	assert.Equal(t, int64(2), pending[0].Version)
	assert.Equal(t, int64(200), pending[0].FeesMinor, "the higher-version edit wins")
}

// A same-version re-enqueue is a tie: higher-version-wins keeps the already-queued
// edit (idempotent, D-016). This pins the merge-arg order in Enqueue — swapping the
// arguments is invisible except on a tie, where it would let the newer payload win.
func TestSyncerCoalesceTieKeepsQueued(t *testing.T) {
	store := newFakeStore()
	s := newSyncer(store, &fakeTransport{offline: true})

	require.NoError(t, s.Enqueue(domain.Operation{ClientRequestID: "a", Version: 5, FeesMinor: 100}))
	require.NoError(t, s.Enqueue(domain.Operation{ClientRequestID: "a", Version: 5, FeesMinor: 999})) // tie

	pending, err := store.Pending()
	require.NoError(t, err)
	require.Len(t, pending, 1)
	assert.Equal(t, int64(100), pending[0].FeesMinor, "a same-version re-enqueue keeps the first-queued edit")
}
