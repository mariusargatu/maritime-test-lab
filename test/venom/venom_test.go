//go:build integration

package venom_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"maritime-test-lab/internal/repopath"
	"maritime-test-lab/internal/testfix"
	estimatorapp "maritime-test-lab/services/estimator/app"
	"maritime-test-lab/services/voyage/app"
)

// Fixed addresses: make test-svc runs -p 1, so no two integration packages race
// for the ports. The estimator gRPC port is unused here (voyage's sync quote goes
// to Gripmock); the estimator runs for its async consumer.
const (
	voyageAddr    = "127.0.0.1:19091"
	estimatorAddr = "127.0.0.1:19002"
	ratePerNm     = 200
)

var fixture struct {
	dsn      string
	stubsURL string
}

// TestMain boots the L2 fixture once: a real Postgres, a Gripmock standing in for
// the estimator, and the voyage service in-process (Q1: in-process app.Run with
// fixture addresses — debuggable, coverage counts). It then writes env.yml and
// the Venom suite runs against it.
func TestMain(m *testing.M) {
	code, err := run(m)
	if err != nil {
		fmt.Fprintln(os.Stderr, "venom testmain:", err)
		os.Exit(1)
	}
	os.Exit(code)
}

func run(m *testing.M) (int, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dsn, stopPG, err := testfix.StartPostgres(ctx)
	if err != nil {
		return 0, err
	}
	defer stopPG()

	brokers, srURL, stopRP, err := testfix.StartRedpanda(ctx)
	if err != nil {
		return 0, err
	}
	defer stopRP()

	gripGRPC, stubsURL, stopGM, err := testfix.StartGripmock(ctx)
	if err != nil {
		return 0, err
	}
	defer stopGM()

	go func() {
		_ = app.Run(ctx, app.Config{
			GRPCAddr:          voyageAddr,
			MetricsAddr:       "127.0.0.1:19101",
			DBDSN:             dsn,
			EstimatorAddr:     gripGRPC,
			KafkaBrokers:      []string{brokers},
			SchemaRegistryURL: srURL,
		})
	}()
	// The real estimator runs only for its async consumer (voyage.created ->
	// estimate.ready). Voyage's sync quote still goes to Gripmock.
	go func() {
		_ = estimatorapp.Run(ctx, estimatorapp.Config{
			GRPCAddr:          estimatorAddr,
			KafkaBrokers:      []string{brokers},
			SchemaRegistryURL: srURL,
			RatePerNmMinor:    ratePerNm,
		})
	}()
	if err := waitServing(voyageAddr, 30*time.Second); err != nil {
		return 0, err
	}
	if err := waitServing(estimatorAddr, 30*time.Second); err != nil {
		return 0, err
	}

	fixture.dsn = dsn
	fixture.stubsURL = stubsURL
	return m.Run(), nil
}

func TestVoyageCreateSuite(t *testing.T) {
	envFile := writeEnvFile(t)

	cmd := exec.Command(venomBin(t), "run", "voyage_create.venom.yml", "--var-from-file="+envFile)
	out, err := cmd.CombinedOutput()
	t.Logf("venom output:\n%s", out)
	require.NoError(t, err, "venom suite failed")
}

// venomBin resolves the standalone venom binary. venom is NOT a go.mod tool
// directive — its protoreflect dependency clashes with buf in a shared module
// graph (D-040) — so make bootstrap installs it into ./bin.
func venomBin(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("VENOM_BIN"); v != "" {
		return v
	}
	candidate, err := repopath.Find("bin/venom")
	if err != nil {
		return "venom" // fall back to PATH
	}
	if _, statErr := os.Stat(candidate); statErr != nil {
		return "venom" // fall back to PATH
	}
	return candidate
}

func writeEnvFile(t *testing.T) string {
	t.Helper()
	content := fmt.Sprintf(
		"voyage_addr: %q\npg_dsn: %q\ngripmock_api: %q\nuuid: %q\nuuid_unavailable: %q\nuuid_async: %q\n",
		voyageAddr, fixture.dsn, fixture.stubsURL, uuid.NewString(), uuid.NewString(), uuid.NewString(),
	)
	path := filepath.Join(t.TempDir(), "env.yml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func waitServing(addr string, timeout time.Duration) error {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	health := healthpb.NewHealthClient(conn)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		resp, err := health.Check(ctx, &healthpb.HealthCheckRequest{})
		cancel()
		if err == nil && resp.GetStatus() == healthpb.HealthCheckResponse_SERVING {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("voyage not SERVING at %s within %s", addr, timeout)
}
