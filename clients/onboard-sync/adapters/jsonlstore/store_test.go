package jsonlstore_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"maritime-test-lab/clients/onboard-sync/adapters/jsonlstore"
	"maritime-test-lab/clients/onboard-sync/domain"
)

func op(id string, version int64) domain.Operation {
	return domain.Operation{ClientRequestID: id, Kind: domain.KindCreate, Origin: "NLRTM", Dest: "SGSIN", DistanceNm: 8200, FeesMinor: 5000, Version: version}
}

func TestStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.jsonl")
	store, err := jsonlstore.Open(path)
	require.NoError(t, err)

	require.NoError(t, store.Append(op("a", 1)))
	require.NoError(t, store.Append(op("b", 1)))

	pending, err := store.Pending()
	require.NoError(t, err)
	require.Len(t, pending, 2)

	require.NoError(t, store.Ack("a"))
	pending, err = store.Pending()
	require.NoError(t, err)
	require.Len(t, pending, 1)
	assert.Equal(t, "b", pending[0].ClientRequestID)
}

// Cold-reopen: a fresh Store over the same file rebuilds the queue — the crash
// survival guarantee.
func TestStoreColdReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.jsonl")

	first, err := jsonlstore.Open(path)
	require.NoError(t, err)
	require.NoError(t, first.Append(op("a", 1)))
	require.NoError(t, first.Append(op("a", 2))) // upsert: higher version
	require.NoError(t, first.Append(op("b", 1)))

	// Abandon `first` (simulated crash) and cold-reopen from disk.
	reopened, err := jsonlstore.Open(path)
	require.NoError(t, err)
	pending, err := reopened.Pending()
	require.NoError(t, err)
	require.Len(t, pending, 2, "queue survives the crash")
	assert.Equal(t, int64(2), pending[0].Version, "the latest write per id replays")

	// An ack persists across another reopen.
	require.NoError(t, reopened.Ack("a"))
	final, err := jsonlstore.Open(path)
	require.NoError(t, err)
	pending, err = final.Pending()
	require.NoError(t, err)
	require.Len(t, pending, 1)
	assert.Equal(t, "b", pending[0].ClientRequestID)
}
