package graph_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"maritime-test-lab/gateway/graph"
	"maritime-test-lab/gen/graphql/model"
	voyagev1 "maritime-test-lab/gen/proto/voyage/v1"
)

// fakeVoyageClient is a hand-written voyagev1.VoyageServiceClient: the resolver's
// only dependency (D-004). Each method returns whatever the test wires in, so we
// can assert how the resolver maps gRPC outcomes into GraphQL responses.
type fakeVoyageClient struct {
	createResp *voyagev1.CreateVoyageResponse
	updateResp *voyagev1.UpdateVoyageResponse
	getResp    *voyagev1.GetVoyageResponse
	err        error
}

func (f fakeVoyageClient) CreateVoyage(context.Context, *voyagev1.CreateVoyageRequest, ...grpc.CallOption) (*voyagev1.CreateVoyageResponse, error) {
	return f.createResp, f.err
}
func (f fakeVoyageClient) UpdateVoyage(context.Context, *voyagev1.UpdateVoyageRequest, ...grpc.CallOption) (*voyagev1.UpdateVoyageResponse, error) {
	return f.updateResp, f.err
}
func (f fakeVoyageClient) GetVoyage(context.Context, *voyagev1.GetVoyageRequest, ...grpc.CallOption) (*voyagev1.GetVoyageResponse, error) {
	return f.getResp, f.err
}

func resolverWith(c voyagev1.VoyageServiceClient) *graph.Resolver {
	return &graph.Resolver{VoyageClient: c}
}

func TestCreateVoyageResolver(t *testing.T) {
	t.Run("rejects an empty clientRequestId before calling the service", func(t *testing.T) {
		r := resolverWith(fakeVoyageClient{err: errors.New("should not be called")})

		_, err := r.Mutation().CreateVoyage(context.Background(), model.CreateVoyageInput{ClientRequestID: ""})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "clientRequestId is required")
	})

	t.Run("passes through estimate_pending degradation", func(t *testing.T) {
		r := resolverWith(fakeVoyageClient{createResp: &voyagev1.CreateVoyageResponse{
			Voyage:          &voyagev1.Voyage{ClientRequestId: "req-1", Version: 1},
			EstimatePending: true,
		}})

		payload, err := r.Mutation().CreateVoyage(context.Background(), model.CreateVoyageInput{
			ClientRequestID: "req-1", Origin: "NLRTM", Dest: "SGSIN", DistanceNm: 8200, FeesMinor: 5000,
		})

		require.NoError(t, err)
		assert.True(t, payload.EstimatePending)
		assert.Equal(t, "req-1", payload.Voyage.ClientRequestID)
	})
}

func TestVoyageQueryResolver(t *testing.T) {
	t.Run("maps a NotFound voyage to a GraphQL null, not an error", func(t *testing.T) {
		r := resolverWith(fakeVoyageClient{err: status.Error(codes.NotFound, "voyage not found")})

		got, err := r.Query().Voyage(context.Background(), "missing")

		require.NoError(t, err, "an unknown voyage is null, not a query error")
		assert.Nil(t, got)
	})

	t.Run("surfaces a non-NotFound failure as an error", func(t *testing.T) {
		r := resolverWith(fakeVoyageClient{err: status.Error(codes.Unavailable, "downstream down")})

		_, err := r.Query().Voyage(context.Background(), "req-1")

		require.Error(t, err)
	})

	t.Run("maps a found voyage to the model", func(t *testing.T) {
		r := resolverWith(fakeVoyageClient{getResp: &voyagev1.GetVoyageResponse{
			Voyage: &voyagev1.Voyage{ClientRequestId: "req-1", Version: 3, EstimateMinor: 1645000},
		}})

		got, err := r.Query().Voyage(context.Background(), "req-1")

		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, 1645000, got.EstimateMinor)
		assert.Equal(t, 3, got.Version)
	})
}
