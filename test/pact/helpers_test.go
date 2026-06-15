//go:build pact

package pact_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"maritime-test-lab/internal/repopath"
)

// DTOs with json tags bridge the Avro events to the JSON content pact matches on.
// Pact has no native Avro; the SR gate covers the wire shape, the message pacts
// cover the logical content (the semantic-vs-shape split).

type voyageCreatedDTO struct {
	ClientRequestID string `json:"client_request_id"`
	Origin          string `json:"origin"`
	Dest            string `json:"dest"`
	DistanceNm      int32  `json:"distance_nm"`
	FeesMinor       int64  `json:"fees_minor"`
	Version         int64  `json:"version"`
}

type estimateReadyDTO struct {
	VoyageID      string `json:"voyage_id"`
	VoyageVersion int64  `json:"voyage_version"`
	AmountMinor   int64  `json:"amount_minor"`
	Currency      string `json:"currency"`
}

// pactDir is where consumer tests write pact files and provider tests read them.
func pactDir(t *testing.T) string {
	t.Helper()
	return repoPath(t, "test/pact/pacts")
}

// repoPath resolves a path relative to the repo root (the directory with go.mod).
func repoPath(t *testing.T, rel string) string {
	t.Helper()
	p, err := repopath.Find(rel)
	require.NoError(t, err)
	return p
}
