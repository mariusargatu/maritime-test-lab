// Package onboardsync wires the onBOARD sync client: a JSONL disk queue plus a
// GraphQL transport, driven by the domain Syncer. It is a library — the L5
// resilience tests construct it in-process and assert via SyncStatus, never by
// watching a container (D-017).
package onboardsync

import (
	"net/http"
	"time"

	"maritime-test-lab/clients/onboard-sync/adapters/graphqltransport"
	"maritime-test-lab/clients/onboard-sync/adapters/jsonlstore"
	"maritime-test-lab/clients/onboard-sync/domain"
)

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

// New builds a Syncer backed by a JSONL queue at queuePath and a GraphQL
// transport to gatewayURL (the full /query URL). requestTimeout bounds each send
// — exceeding it is how the client detects it is offline.
func New(gatewayURL, queuePath string, requestTimeout time.Duration) (*domain.Syncer, error) {
	store, err := jsonlstore.Open(queuePath)
	if err != nil {
		return nil, err
	}
	transport := graphqltransport.New(gatewayURL, &http.Client{}, requestTimeout)
	retry := domain.NewRetryPolicy(100*time.Millisecond, 30*time.Second, 1)
	return domain.NewSyncer(store, transport, systemClock{}, retry), nil
}
