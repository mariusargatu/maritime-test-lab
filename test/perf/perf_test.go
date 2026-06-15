//go:build perf

package perf

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"

	graphqlops "maritime-test-lab/clients/onboard-sync/graphql"
)

const (
	gatewayURL  = "http://localhost:18080/query"
	broker      = "localhost:19092"
	loadVoyages = 50
)

// The perf gate: a burst of createVoyage load must keep the error rate under 1%
// and the async consumer lag must drain to ~0 within 30s of the burst ending.
// Latency is deliberately not gated (D-019).
func TestEstimatePipelinePerfGates(t *testing.T) {
	ctx := context.Background()

	var errCount int64
	var wg sync.WaitGroup
	for i := 0; i < loadVoyages; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := createVoyage(uuid.NewString()); err != nil {
				atomic.AddInt64(&errCount, 1)
			}
		}()
	}
	wg.Wait()

	errorRate := float64(atomic.LoadInt64(&errCount)) / float64(loadVoyages)
	assert.Less(t, errorRate, 0.01, "error rate must stay under 1%% (got %.3f)", errorRate)

	client, err := kgo.NewClient(kgo.SeedBrokers(broker))
	require.NoError(t, err)
	t.Cleanup(client.Close)
	admin := kadm.NewClient(client)

	require.Eventually(t, func() bool {
		return totalLag(ctx, admin, "estimator-service") == 0 && totalLag(ctx, admin, "voyage-service") == 0
	}, 30*time.Second, time.Second, "consumer lag must drain to ~0 after the burst")
}

func totalLag(ctx context.Context, admin *kadm.Client, group string) int64 {
	lags, err := admin.Lag(ctx, group)
	if err != nil {
		return -1
	}
	described, ok := lags[group]
	if !ok || described.DescribeErr != nil || described.FetchErr != nil {
		return -1
	}
	return described.Lag.Total()
}

func createVoyage(id string) error {
	body, err := json.Marshal(map[string]any{
		"query": graphqlops.CreateVoyage,
		"variables": map[string]any{
			"input": map[string]any{"clientRequestId": id, "origin": "NLRTM", "dest": "SGSIN", "distanceNm": 8200, "feesMinor": 5000},
		},
	})
	if err != nil {
		return err
	}
	resp, err := http.Post(gatewayURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	payload, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d: %s", resp.StatusCode, payload)
	}
	var parsed struct {
		Errors []json.RawMessage `json:"errors"`
	}
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return err
	}
	if len(parsed.Errors) > 0 {
		return fmt.Errorf("graphql errors: %s", payload)
	}
	return nil
}
