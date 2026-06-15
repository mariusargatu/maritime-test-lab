// Package grpcserver is the voyage service's inbound adapter: it implements the
// generated VoyageService gRPC interface, validates at the boundary, and drives
// the domain ports (CreateVoyage, UpdateVoyage with higher-version-wins merge
// per D-016, and GetVoyage).
package grpcserver

import (
	"context"
	"errors"
	"log"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	voyagev1 "maritime-test-lab/gen/proto/voyage/v1"
	"maritime-test-lab/services/voyage/domain"
)

// internalErr logs the underlying cause server-side and returns a clean Internal
// status. The caller never sees infra detail (CLAUDE.md: never leak internal
// detail to the caller); operators get the full error in the logs.
func internalErr(op string, err error) error {
	log.Printf("voyage grpc: %s: %v", op, err)
	return status.Errorf(codes.Internal, "%s failed", op)
}

// Server implements voyagev1.VoyageServiceServer over the domain ports.
type Server struct {
	voyagev1.UnimplementedVoyageServiceServer
	repo      domain.VoyageRepository
	estimator domain.Estimator
}

// New constructs the VoyageService server with its dependencies injected.
func New(repo domain.VoyageRepository, estimator domain.Estimator) *Server {
	return &Server{repo: repo, estimator: estimator}
}

// CreateVoyage validates, dedupes (insert-or-return-existing by
// client_request_id), and returns a best-effort provisional quote. The quote is
// NOT persisted — the authoritative estimate arrives over Kafka (Phase 3). If the
// estimator is unavailable the voyage still succeeds with estimate_pending=true.
func (s *Server) CreateVoyage(ctx context.Context, req *voyagev1.CreateVoyageRequest) (*voyagev1.CreateVoyageResponse, error) {
	v := domain.Voyage{
		ClientRequestID: req.GetClientRequestId(),
		Origin:          req.GetOrigin(),
		Dest:            req.GetDest(),
		DistanceNm:      req.GetDistanceNm(),
		FeesMinor:       req.GetFeesMinor(),
	}
	if err := v.Validate(); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	created, err := s.repo.Create(ctx, v)
	if err != nil {
		return nil, internalErr("create voyage", err)
	}

	resp := &voyagev1.CreateVoyageResponse{Voyage: toProto(created)}
	quote, err := s.estimator.Estimate(ctx, created)
	if err != nil {
		if errors.Is(err, domain.ErrEstimatorUnavailable) {
			resp.EstimatePending = true
			return resp, nil
		}
		return nil, internalErr("quote voyage", err)
	}
	resp.ProvisionalEstimateMinor = quote.Minor
	return resp, nil
}

// UpdateVoyage applies the higher-version-wins merge rule (D-016): a stale write
// (version not strictly higher) is a no-op and the current state is returned.
func (s *Server) UpdateVoyage(ctx context.Context, req *voyagev1.UpdateVoyageRequest) (*voyagev1.UpdateVoyageResponse, error) {
	in := req.GetVoyage()
	v := domain.Voyage{
		ClientRequestID: in.GetClientRequestId(),
		Origin:          in.GetOrigin(),
		Dest:            in.GetDest(),
		DistanceNm:      in.GetDistanceNm(),
		FeesMinor:       in.GetFeesMinor(),
		Version:         in.GetVersion(),
	}
	if err := v.Validate(); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	current, err := s.repo.Update(ctx, v)
	if err != nil {
		if errors.Is(err, domain.ErrVoyageNotFound) {
			return nil, status.Error(codes.NotFound, "voyage not found")
		}
		return nil, internalErr("update voyage", err)
	}
	return &voyagev1.UpdateVoyageResponse{Voyage: toProto(current)}, nil
}

// GetVoyage returns the current persisted voyage, including its authoritative
// estimate once applied.
func (s *Server) GetVoyage(ctx context.Context, req *voyagev1.GetVoyageRequest) (*voyagev1.GetVoyageResponse, error) {
	if req.GetClientRequestId() == "" {
		return nil, status.Error(codes.InvalidArgument, "client_request_id required")
	}
	v, err := s.repo.Get(ctx, req.GetClientRequestId())
	if err != nil {
		if errors.Is(err, domain.ErrVoyageNotFound) {
			return nil, status.Error(codes.NotFound, "voyage not found")
		}
		return nil, internalErr("get voyage", err)
	}
	return &voyagev1.GetVoyageResponse{Voyage: toProto(v)}, nil
}

func toProto(v domain.Voyage) *voyagev1.Voyage {
	return &voyagev1.Voyage{
		ClientRequestId: v.ClientRequestID,
		Origin:          v.Origin,
		Dest:            v.Dest,
		DistanceNm:      v.DistanceNm,
		FeesMinor:       v.FeesMinor,
		Version:         v.Version,
		EstimateMinor:   v.EstimateMinor,
	}
}
