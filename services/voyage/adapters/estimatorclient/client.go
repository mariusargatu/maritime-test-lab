// Package estimatorclient is voyage's outbound adapter to the estimator gRPC
// service. It implements the voyage domain.Estimator port by calling the
// generated client and translating infra errors into domain errors, so the
// domain never sees a grpc type.
package estimatorclient

import (
	"context"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	estimatorv1 "maritime-test-lab/gen/proto/estimator/v1"
	"maritime-test-lab/internal/money"
	"maritime-test-lab/services/voyage/domain"
)

// Adapter implements domain.Estimator over a gRPC client.
type Adapter struct {
	client estimatorv1.EstimatorServiceClient
}

// New wraps a generated estimator gRPC client.
func New(client estimatorv1.EstimatorServiceClient) *Adapter {
	return &Adapter{client: client}
}

// Estimate maps the voyage to an EstimateRequest, calls the service, and returns
// the quote as Money. A gRPC Unavailable becomes domain.ErrEstimatorUnavailable.
func (a *Adapter) Estimate(ctx context.Context, v domain.Voyage) (money.Money, error) {
	resp, err := a.client.Estimate(ctx, &estimatorv1.EstimateRequest{
		ClientRequestId: v.ClientRequestID,
		DistanceNm:      v.DistanceNm,
		FeesMinor:       v.FeesMinor,
	})
	if err != nil {
		if status.Code(err) == codes.Unavailable {
			return money.Money{}, fmt.Errorf("estimate %s: %w", v.ClientRequestID, domain.ErrEstimatorUnavailable)
		}
		return money.Money{}, fmt.Errorf("estimate %s: %w", v.ClientRequestID, err)
	}
	return money.Money{Minor: resp.GetAmountMinor(), Currency: resp.GetCurrency()}, nil
}
