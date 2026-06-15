//go:build pact

package pact_test

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	message "github.com/pact-foundation/pact-go/v2/message/v4"
	"github.com/pact-foundation/pact-go/v2/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"maritime-test-lab/gateway/graph"
	"maritime-test-lab/gen/graphql/model"
	voyagev1 "maritime-test-lab/gen/proto/voyage/v1"
	"maritime-test-lab/internal/money"
	voyagegrpc "maritime-test-lab/services/voyage/adapters/grpcserver"
	voyagedomain "maritime-test-lab/services/voyage/domain"
)

// Consumer pact for the gateway→voyage edge: the REAL GraphQL resolver drives the
// voyage gRPC client against a pact mock built from the voyage proto.
func TestGatewayVoyageGrpcConsumer(t *testing.T) {
	p, err := message.NewSynchronousPact(message.Config{Consumer: "gateway", Provider: "voyage", PactDir: pactDir(t)})
	require.NoError(t, err)

	protoPath := repoPath(t, "proto/voyage/v1/voyage.proto")
	interaction := `{
		"pact:proto": "` + protoPath + `",
		"pact:proto-service": "VoyageService/CreateVoyage",
		"pact:content-type": "application/protobuf",
		"request": {
			"client_request_id": "notEmpty('req-1')",
			"origin": "notEmpty('NLRTM')",
			"dest": "notEmpty('SGSIN')",
			"distance_nm": "matching(number, 8200)",
			"fees_minor": "matching(number, 5000)"
		},
		"response": {
			"voyage": { "client_request_id": "notEmpty('req-1')", "version": "matching(number, 1)" },
			"provisional_estimate_minor": "matching(number, 1645000)",
			"estimate_pending": "matching(boolean, false)"
		}
	}`

	err = p.AddSynchronousMessage("gateway creates a voyage").
		UsingPlugin(message.PluginConfig{Plugin: "protobuf", Version: "0.8.0"}).
		WithContents(interaction, "application/protobuf").
		StartTransport("grpc", "127.0.0.1", nil).
		ExecuteTest(t, func(transport message.TransportConfig, _ message.SynchronousMessage) error {
			conn, err := grpc.NewClient(fmt.Sprintf("127.0.0.1:%d", transport.Port), grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				return err
			}
			defer func() { _ = conn.Close() }()

			resolver := &graph.Resolver{VoyageClient: voyagev1.NewVoyageServiceClient(conn)}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			payload, err := resolver.Mutation().CreateVoyage(ctx, model.CreateVoyageInput{
				ClientRequestID: "req-1", Origin: "NLRTM", Dest: "SGSIN", DistanceNm: 8200, FeesMinor: 5000,
			})
			if err != nil {
				return err
			}
			assert.Equal(t, "req-1", payload.Voyage.ClientRequestID)
			assert.False(t, payload.EstimatePending)
			return nil
		})
	require.NoError(t, err)
}

// Consumer pact for the gateway→voyage UPDATE edge (higher-version-wins, D-016):
// the REAL resolver drives UpdateVoyage and expects the merged voyage back. Adds a
// second interaction to the same pact file; the provider verify below covers both.
func TestGatewayVoyageUpdateGrpcConsumer(t *testing.T) {
	p, err := message.NewSynchronousPact(message.Config{Consumer: "gateway", Provider: "voyage", PactDir: pactDir(t)})
	require.NoError(t, err)

	protoPath := repoPath(t, "proto/voyage/v1/voyage.proto")
	interaction := `{
		"pact:proto": "` + protoPath + `",
		"pact:proto-service": "VoyageService/UpdateVoyage",
		"pact:content-type": "application/protobuf",
		"request": {
			"voyage": {
				"client_request_id": "notEmpty('req-1')",
				"origin": "notEmpty('NLRTM')",
				"dest": "notEmpty('SGSIN')",
				"distance_nm": "matching(number, 8200)",
				"fees_minor": "matching(number, 5000)",
				"version": "matching(number, 2)"
			}
		},
		"response": {
			"voyage": { "client_request_id": "notEmpty('req-1')", "version": "matching(number, 2)" }
		}
	}`

	err = p.AddSynchronousMessage("gateway updates a voyage").
		UsingPlugin(message.PluginConfig{Plugin: "protobuf", Version: "0.8.0"}).
		WithContents(interaction, "application/protobuf").
		StartTransport("grpc", "127.0.0.1", nil).
		ExecuteTest(t, func(transport message.TransportConfig, _ message.SynchronousMessage) error {
			conn, err := grpc.NewClient(fmt.Sprintf("127.0.0.1:%d", transport.Port), grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				return err
			}
			defer func() { _ = conn.Close() }()

			resolver := &graph.Resolver{VoyageClient: voyagev1.NewVoyageServiceClient(conn)}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			updated, err := resolver.Mutation().UpdateVoyage(ctx, model.UpdateVoyageInput{
				ClientRequestID: "req-1", Origin: "NLRTM", Dest: "SGSIN", DistanceNm: 8200, FeesMinor: 5000, Version: 2,
			})
			if err != nil {
				return err
			}
			assert.Equal(t, "req-1", updated.ClientRequestID)
			assert.Equal(t, 2, updated.Version)
			return nil
		})
	require.NoError(t, err)
}

// Provider verify: the REAL voyage gRPC server (with port fakes — no DB/Kafka)
// must satisfy the gateway's pact. The fake repo is idempotent by construction,
// which is the "voyage X exists → replay returns the same voyage" provider state.
func TestVoyageGrpcProvider(t *testing.T) {
	port := startVoyageProvider(t)

	err := provider.NewVerifier().VerifyProvider(t, provider.VerifyRequest{
		Provider:        "voyage",
		ProviderBaseURL: fmt.Sprintf("http://127.0.0.1:%d", port),
		Transports:      []provider.Transport{{Protocol: "grpc", Port: uint16(port)}},
		PactFiles:       []string{repoPath(t, "test/pact/pacts/gateway-voyage.json")},
	})
	require.NoError(t, err)
}

func startVoyageProvider(t *testing.T) int {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	server := grpc.NewServer()
	voyagev1.RegisterVoyageServiceServer(server, voyagegrpc.New(fakeVoyageRepo{}, fakeVoyageEstimator{}))
	go func() { _ = server.Serve(lis) }()
	t.Cleanup(server.GracefulStop)

	return lis.Addr().(*net.TCPAddr).Port
}

type fakeVoyageRepo struct{}

func (fakeVoyageRepo) Create(_ context.Context, v voyagedomain.Voyage) (voyagedomain.Voyage, error) {
	v.Version = 1
	return v, nil
}

func (fakeVoyageRepo) Update(_ context.Context, v voyagedomain.Voyage) (voyagedomain.Voyage, error) {
	return v, nil
}

func (fakeVoyageRepo) Get(_ context.Context, id string) (voyagedomain.Voyage, error) {
	return voyagedomain.Voyage{ClientRequestID: id, Origin: "NLRTM", Dest: "SGSIN", DistanceNm: 8200, FeesMinor: 5000, Version: 1, EstimateMinor: 1645000}, nil
}

type fakeVoyageEstimator struct{}

func (fakeVoyageEstimator) Estimate(_ context.Context, _ voyagedomain.Voyage) (money.Money, error) {
	return money.FromUSD(1645000), nil
}
