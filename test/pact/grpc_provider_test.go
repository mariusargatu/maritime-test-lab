//go:build pact

package pact_test

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/pact-foundation/pact-go/v2/message"
	"github.com/pact-foundation/pact-go/v2/models"
	"github.com/pact-foundation/pact-go/v2/provider"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"maritime-test-lab/gen/avro"
	estimatorv1 "maritime-test-lab/gen/proto/estimator/v1"
	"maritime-test-lab/internal/money"
	estimatorgrpc "maritime-test-lab/services/estimator/adapters/grpcserver"
	estimatorkafka "maritime-test-lab/services/estimator/adapters/kafka"
	estimatordomain "maritime-test-lab/services/estimator/domain"
	"maritime-test-lab/services/voyage/adapters/kafka"
	"maritime-test-lab/services/voyage/domain"
)

const providerRate = 200

// Provider verify for everything voyage consumes from estimator: the sync
// Estimate RPC (real gRPC server) AND the estimate.ready message (real builder).
// Both live in voyage-estimator.json (same consumer-provider pair).
func TestEstimatorProvider(t *testing.T) {
	port := startEstimatorProvider(t)

	err := provider.NewVerifier().VerifyProvider(t, provider.VerifyRequest{
		Provider:        "estimator",
		ProviderBaseURL: fmt.Sprintf("http://127.0.0.1:%d", port),
		Transports:      []provider.Transport{{Protocol: "grpc", Port: uint16(port)}},
		PactFiles:       []string{repoPath(t, "test/pact/pacts/voyage-estimator.json")},
		MessageHandlers: message.Handlers{
			"an estimate.ready event": func(_ []models.ProviderState) (message.Body, message.Metadata, error) {
				vc := avro.VoyageCreated{ClientRequestID: "11111111-1111-1111-1111-111111111111", Version: 1, DistanceNm: 8200, FeesMinor: 5000}
				cost, err := estimatordomain.EstimateCost(vc.DistanceNm, money.FromUSD(vc.FeesMinor), money.FromUSD(providerRate))
				if err != nil {
					return nil, nil, err
				}
				e := estimatorkafka.BuildEstimateReady(vc, cost, time.UnixMilli(1_700_000_000_000))
				return estimateReadyDTO{VoyageID: e.VoyageID, VoyageVersion: e.VoyageVersion, AmountMinor: e.AmountMinor, Currency: e.Currency},
					message.Metadata{"contentType": "application/json"}, nil
			},
		},
	})
	require.NoError(t, err)
}

// Provider verify for voyage.created (real event-builder).
func TestVoyageProvider(t *testing.T) {
	err := provider.NewVerifier().VerifyProvider(t, provider.VerifyRequest{
		Provider:  "voyage",
		PactFiles: []string{repoPath(t, "test/pact/pacts/estimator-voyage.json")},
		MessageHandlers: message.Handlers{
			"a voyage.created event": func(_ []models.ProviderState) (message.Body, message.Metadata, error) {
				e := kafka.ToVoyageCreated(domain.Voyage{
					ClientRequestID: "11111111-1111-1111-1111-111111111111",
					Origin:          "NLRTM", Dest: "SGSIN", DistanceNm: 8200, FeesMinor: 5000, Version: 1,
				})
				return voyageCreatedDTO{
						ClientRequestID: e.ClientRequestID, Origin: e.Origin, Dest: e.Dest,
						DistanceNm: e.DistanceNm, FeesMinor: e.FeesMinor, Version: e.Version,
					},
					message.Metadata{"contentType": "application/json"}, nil
			},
		},
	})
	require.NoError(t, err)
}

func startEstimatorProvider(t *testing.T) int {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	server := grpc.NewServer()
	estimatorv1.RegisterEstimatorServiceServer(server, estimatorgrpc.New(money.FromUSD(providerRate)))
	go func() { _ = server.Serve(lis) }()
	t.Cleanup(server.GracefulStop)

	return lis.Addr().(*net.TCPAddr).Port
}
