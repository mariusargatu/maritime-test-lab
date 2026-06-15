//go:build resilience

package resilience

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	onboardsync "maritime-test-lab/clients/onboard-sync"
	"maritime-test-lab/clients/onboard-sync/domain"
	graphqlops "maritime-test-lab/clients/onboard-sync/graphql"
)

const (
	toxiproxyAdmin      = "http://localhost:8474"
	gatewayViaToxiproxy = "http://localhost:8666/query"  // the client writes through here (degradable)
	gatewayDirect       = "http://localhost:18080/query" // the test reads here (stable)
	voyageMetrics       = "http://localhost:19100/metrics"
	requestTimeout      = 2 * time.Second
)

var tp = newToxiproxy(toxiproxyAdmin, "gateway")

func TestMain(m *testing.M) {
	if err := tp.ensureProxy("0.0.0.0:8666", "gateway:8080"); err != nil {
		fmt.Fprintln(os.Stderr, "resilience: toxiproxy setup:", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// Scenario 1 (timeout black-hole): partition → enqueue N → heal → all applied.
func TestOfflineQueueDrainsAfterHeal(t *testing.T) {
	require.NoError(t, tp.reset())
	syncer := newClient(t, t.TempDir())

	idA, idB := uuid.NewString(), uuid.NewString()
	require.NoError(t, syncer.Enqueue(createOp(idA)))
	require.NoError(t, syncer.Enqueue(createOp(idB)))

	require.NoError(t, tp.addToxic("blackhole", "timeout", map[string]any{"timeout": 0}))
	synced, err := syncer.SyncOnce(context.Background())
	require.NoError(t, err)
	assert.Zero(t, synced, "nothing syncs while partitioned")
	st, _ := syncer.SyncStatus()
	assert.Equal(t, 2, st.QueueDepth)
	assert.NotEmpty(t, st.LastError)

	require.NoError(t, tp.reset()) // heal
	drain(t, syncer)

	assert.NotNil(t, graphqlVoyage(t, idA), "voyage A applied after heal")
	assert.NotNil(t, graphqlVoyage(t, idB), "voyage B applied after heal")
}

// Scenario 2 (reset_peer RST): partition → enqueue → abandon instance →
// cold-reopen → heal → nothing lost, no dupes.
func TestCrashSurvivalColdReopen(t *testing.T) {
	require.NoError(t, tp.reset())
	queuePath := filepath.Join(t.TempDir(), "queue.jsonl")
	id := uuid.NewString()

	require.NoError(t, tp.addToxic("rst", "reset_peer", map[string]any{"timeout": 0}))
	crashed, err := onboardsync.New(gatewayViaToxiproxy, queuePath, requestTimeout)
	require.NoError(t, err)
	require.NoError(t, crashed.Enqueue(createOp(id)))
	_, _ = crashed.SyncOnce(context.Background()) // fails (RST) -> stays queued
	st, _ := crashed.SyncStatus()
	require.Equal(t, 1, st.QueueDepth)

	// Abandon the instance entirely; cold-reopen the disk queue from a fresh one.
	reopened, err := onboardsync.New(gatewayViaToxiproxy, queuePath, requestTimeout)
	require.NoError(t, err)
	st, _ = reopened.SyncStatus()
	require.Equal(t, 1, st.QueueDepth, "the queue survived the crash")

	require.NoError(t, tp.reset()) // heal
	drain(t, reopened)
	assert.NotNil(t, graphqlVoyage(t, id), "the queued write is applied, not lost")
}

// Scenario 3: concurrent edits resolve by higher-version-wins, read via GraphQL.
func TestConcurrentEditsHigherVersionWins(t *testing.T) {
	require.NoError(t, tp.reset())
	syncer := newClient(t, t.TempDir())
	id := uuid.NewString()

	require.NoError(t, syncer.Enqueue(createOp(id)))
	drain(t, syncer)

	require.NoError(t, syncer.Enqueue(updateOp(id, 3, 300))) // higher
	drain(t, syncer)
	require.NoError(t, syncer.Enqueue(updateOp(id, 2, 200))) // stale, must lose
	drain(t, syncer)

	v := graphqlVoyage(t, id)
	require.NotNil(t, v)
	assert.Equal(t, int64(3), v.Version, "the higher version wins server-side")
}

// Scenario 4: the dedupe metric series is present and advances on a replay — the
// series-present guard (scrapeMetric fails if the name changes), rehearsal for P7.
func TestDedupeMetricSeriesPresent(t *testing.T) {
	require.NoError(t, tp.reset())
	syncer := newClient(t, t.TempDir())
	id := uuid.NewString()

	before := scrapeMetric(t, "voyage_create_dedupe_total")

	require.NoError(t, syncer.Enqueue(createOp(id)))
	drain(t, syncer)
	require.NoError(t, syncer.Enqueue(createOp(id))) // same id again -> server dedupes
	drain(t, syncer)

	after := scrapeMetric(t, "voyage_create_dedupe_total")
	assert.Greater(t, after, before, "dedupe counter advanced on the idempotent replay")
}

// --- helpers ---

func newClient(t *testing.T, dir string) *domain.Syncer {
	t.Helper()
	s, err := onboardsync.New(gatewayViaToxiproxy, filepath.Join(dir, "queue.jsonl"), requestTimeout)
	require.NoError(t, err)
	return s
}

func createOp(id string) domain.Operation {
	return domain.Operation{ClientRequestID: id, Kind: domain.KindCreate, Origin: "NLRTM", Dest: "SGSIN", DistanceNm: 100, FeesMinor: 5000, Version: 1}
}

func updateOp(id string, version, fees int64) domain.Operation {
	return domain.Operation{ClientRequestID: id, Kind: domain.KindUpdate, Origin: "NLRTM", Dest: "SGSIN", DistanceNm: 100, FeesMinor: fees, Version: version}
}

// drain syncs until the queue empties — require.Eventually, never sleep.
func drain(t *testing.T, syncer *domain.Syncer) {
	t.Helper()
	require.Eventually(t, func() bool {
		if _, err := syncer.SyncOnce(context.Background()); err != nil {
			return false
		}
		st, err := syncer.SyncStatus()
		return err == nil && st.QueueDepth == 0
	}, 30*time.Second, 300*time.Millisecond, "queue should drain after the network heals")
}

type voyageView struct {
	ClientRequestID string `json:"clientRequestId"`
	Version         int64  `json:"version"`
	EstimateMinor   int64  `json:"estimateMinor"`
}

func graphqlVoyage(t *testing.T, id string) *voyageView {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"query":     graphqlops.GetVoyage,
		"variables": map[string]any{"clientRequestId": id},
	})
	require.NoError(t, err)

	resp, err := http.Post(gatewayDirect, "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	payload, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var parsed struct {
		Data struct {
			Voyage *voyageView `json:"voyage"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(payload, &parsed), "body: %s", payload)
	return parsed.Data.Voyage
}

// scrapeMetric parses the Prometheus text exposition for a label-free counter.
// It fails if the series is absent — the series-present guard (a renamed metric
// breaks the test), rehearsal for P7.
func scrapeMetric(t *testing.T, name string) float64 {
	t.Helper()
	resp, err := http.Get(voyageMetrics)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	payload, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	for _, line := range strings.Split(string(payload), "\n") {
		if strings.HasPrefix(line, "#") || !strings.HasPrefix(line, name+" ") {
			continue
		}
		fields := strings.Fields(line)
		value, err := strconv.ParseFloat(fields[len(fields)-1], 64)
		require.NoError(t, err)
		return value
	}
	t.Fatalf("metric %s not present — the series-present guard", name)
	return 0
}
