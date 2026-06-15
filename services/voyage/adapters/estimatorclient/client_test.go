package estimatorclient

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	estimatorv1 "maritime-test-lab/gen/proto/estimator/v1"
	"maritime-test-lab/internal/money"
	"maritime-test-lab/services/voyage/domain"
)

// gomock (not a hand fake) earns its place here: the point of the adapter test is
// the interaction — the call is made once, request fields map correctly, and an
// infra error becomes a domain error.

func TestAdapterEstimate(t *testing.T) {
	voyage := domain.Voyage{ClientRequestID: "req-1", Origin: "NLRTM", Dest: "SGSIN", DistanceNm: 8200, FeesMinor: 5000}

	t.Run("maps request fields, calls once, returns Money", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		client := NewMockEstimatorServiceClient(ctrl)

		var captured *estimatorv1.EstimateRequest
		client.EXPECT().
			Estimate(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, req *estimatorv1.EstimateRequest, _ ...grpc.CallOption) (*estimatorv1.EstimateResponse, error) {
				captured = req
				return &estimatorv1.EstimateResponse{AmountMinor: 25000, Currency: money.USD}, nil
			}).
			Times(1)

		got, err := New(client).Estimate(context.Background(), voyage)

		require.NoError(t, err)
		assert.Equal(t, money.FromUSD(25000), got)
		// fields mapped from the voyage to the request
		assert.Equal(t, "req-1", captured.GetClientRequestId())
		assert.Equal(t, int32(8200), captured.GetDistanceNm())
		assert.Equal(t, int64(5000), captured.GetFeesMinor())
	})

	t.Run("translates codes.Unavailable to a domain error", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		client := NewMockEstimatorServiceClient(ctrl)

		client.EXPECT().
			Estimate(gomock.Any(), gomock.Any()).
			Return(nil, status.Error(codes.Unavailable, "estimator down")).
			Times(1)

		_, err := New(client).Estimate(context.Background(), voyage)

		require.ErrorIs(t, err, domain.ErrEstimatorUnavailable)
	})
}
