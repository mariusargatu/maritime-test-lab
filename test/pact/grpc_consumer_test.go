//go:build pact

package pact_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	message "github.com/pact-foundation/pact-go/v2/message/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	estimatorv1 "maritime-test-lab/gen/proto/estimator/v1"
	"maritime-test-lab/services/voyage/adapters/estimatorclient"
	"maritime-test-lab/services/voyage/domain"
)

// Consumer pact for the sync edge: voyage's estimator-client adapter ↔ a pact
// gRPC mock built from the estimator proto (protobuf plugin). Driving the REAL
// adapter is the point — it proves the adapter speaks the contract, not a
// hand-written stub. Writes test/pact/pacts/voyage-estimator.json.
func TestVoyageEstimatorGrpcConsumer(t *testing.T) {
	p, err := message.NewSynchronousPact(message.Config{
		Consumer: "voyage",
		Provider: "estimator",
		PactDir:  pactDir(t),
	})
	require.NoError(t, err)

	protoPath := repoPath(t, "proto/estimator/v1/estimator.proto")
	interaction := `{
		"pact:proto": "` + protoPath + `",
		"pact:proto-service": "EstimatorService/Estimate",
		"pact:content-type": "application/protobuf",
		"request": {
			"client_request_id": "notEmpty('req-1')",
			"distance_nm": "matching(number, 8200)",
			"fees_minor": "matching(number, 5000)"
		},
		"response": {
			"amount_minor": "matching(number, 1645000)",
			"currency": "matching(type, 'USD')"
		}
	}`

	err = p.AddSynchronousMessage("voyage requests an estimate for a voyage").
		UsingPlugin(message.PluginConfig{Plugin: "protobuf", Version: "0.8.0"}).
		WithContents(interaction, "application/protobuf").
		StartTransport("grpc", "127.0.0.1", nil).
		ExecuteTest(t, func(transport message.TransportConfig, _ message.SynchronousMessage) error {
			conn, err := grpc.NewClient(fmt.Sprintf("127.0.0.1:%d", transport.Port), grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				return err
			}
			defer func() { _ = conn.Close() }()

			adapter := estimatorclient.New(estimatorv1.NewEstimatorServiceClient(conn))
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			quote, err := adapter.Estimate(ctx, domain.Voyage{ClientRequestID: "req-1", DistanceNm: 8200, FeesMinor: 5000})
			if err != nil {
				return err
			}
			assert.Equal(t, int64(1645000), quote.Minor)
			assert.Equal(t, "USD", quote.Currency)
			return nil
		})
	require.NoError(t, err)
}
