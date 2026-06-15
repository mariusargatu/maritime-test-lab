// Package grpcserver is the estimator service's inbound adapter: the sync
// Estimate RPC (voyage's provisional quote). The authoritative async path runs
// over Kafka (adapters/kafka).
package grpcserver

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	estimatorv1 "maritime-test-lab/gen/proto/estimator/v1"
	"maritime-test-lab/internal/money"
	"maritime-test-lab/services/estimator/domain"
)

// Server implements estimatorv1.EstimatorServiceServer.
type Server struct {
	estimatorv1.UnimplementedEstimatorServiceServer
	ratePerNm money.Money
}

// New constructs the EstimatorService server with the configured per-nm rate.
func New(ratePerNm money.Money) *Server {
	return &Server{ratePerNm: ratePerNm}
}

// Estimate prices a voyage leg synchronously. Invalid inputs (negative distance,
// overflow) are boundary errors.
func (s *Server) Estimate(_ context.Context, req *estimatorv1.EstimateRequest) (*estimatorv1.EstimateResponse, error) {
	cost, err := domain.EstimateCost(req.GetDistanceNm(), money.FromUSD(req.GetFeesMinor()), s.ratePerNm)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	return &estimatorv1.EstimateResponse{AmountMinor: cost.Minor, Currency: cost.Currency}, nil
}
