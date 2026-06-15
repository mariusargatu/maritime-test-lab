package grpcserver_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	voyagev1 "maritime-test-lab/gen/proto/voyage/v1"
	"maritime-test-lab/internal/money"
	"maritime-test-lab/services/voyage/adapters/grpcserver"
	"maritime-test-lab/services/voyage/domain"
)

// These tests pin the boundary adapter's one job: translate domain outcomes into
// the right gRPC status codes (and the estimate_pending degradation). It is pure
// branching over the ports, so hand-written fakes and zero I/O suffice — the seam
// the rest of the pyramid trusts but never exercises in isolation.

type stubRepo struct {
	createFn    func(domain.Voyage) (domain.Voyage, error)
	updateFn    func(domain.Voyage) (domain.Voyage, error)
	getFn       func(string) (domain.Voyage, error)
	createCalls int
}

func (s *stubRepo) Create(_ context.Context, v domain.Voyage) (domain.Voyage, error) {
	s.createCalls++
	return s.createFn(v)
}
func (s *stubRepo) Update(_ context.Context, v domain.Voyage) (domain.Voyage, error) {
	return s.updateFn(v)
}
func (s *stubRepo) Get(_ context.Context, id string) (domain.Voyage, error) { return s.getFn(id) }

type stubEstimator struct {
	quote money.Money
	err   error
	calls int
}

func (s *stubEstimator) Estimate(context.Context, domain.Voyage) (money.Money, error) {
	s.calls++
	return s.quote, s.err
}

func echoCreate(v domain.Voyage) (domain.Voyage, error) { v.Version = 1; return v, nil }

func validCreateReq() *voyagev1.CreateVoyageRequest {
	return &voyagev1.CreateVoyageRequest{
		ClientRequestId: "req-1", Origin: "NLRTM", Dest: "SGSIN", DistanceNm: 8200, FeesMinor: 5000,
	}
}

func TestCreateVoyage(t *testing.T) {
	t.Run("rejects invalid input with InvalidArgument and calls nothing downstream", func(t *testing.T) {
		repo := &stubRepo{createFn: echoCreate}
		est := &stubEstimator{quote: money.FromUSD(1)}
		req := validCreateReq()
		req.ClientRequestId = "" // fails domain.Validate

		_, err := grpcserver.New(repo, est).CreateVoyage(context.Background(), req)

		assert.Equal(t, codes.InvalidArgument, status.Code(err))
		assert.Zero(t, repo.createCalls, "must not persist invalid input")
		assert.Zero(t, est.calls, "must not quote invalid input")
	})

	t.Run("maps a repository failure to Internal", func(t *testing.T) {
		repo := &stubRepo{createFn: func(domain.Voyage) (domain.Voyage, error) {
			return domain.Voyage{}, errors.New("connection refused")
		}}
		est := &stubEstimator{quote: money.FromUSD(1)}

		_, err := grpcserver.New(repo, est).CreateVoyage(context.Background(), validCreateReq())

		assert.Equal(t, codes.Internal, status.Code(err))
		assert.NotContains(t, status.Convert(err).Message(), "connection refused", "infra detail must not leak to the caller")
	})

	t.Run("estimator unavailable still succeeds with estimate_pending", func(t *testing.T) {
		repo := &stubRepo{createFn: echoCreate}
		est := &stubEstimator{err: domain.ErrEstimatorUnavailable}

		resp, err := grpcserver.New(repo, est).CreateVoyage(context.Background(), validCreateReq())

		require.NoError(t, err)
		assert.True(t, resp.GetEstimatePending(), "degrade, do not fail, when the estimator is down")
		assert.Zero(t, resp.GetProvisionalEstimateMinor())
	})

	t.Run("an unexpected estimator error is Internal", func(t *testing.T) {
		repo := &stubRepo{createFn: echoCreate}
		est := &stubEstimator{err: errors.New("boom")}

		_, err := grpcserver.New(repo, est).CreateVoyage(context.Background(), validCreateReq())

		assert.Equal(t, codes.Internal, status.Code(err))
	})

	t.Run("happy path returns the provisional quote", func(t *testing.T) {
		repo := &stubRepo{createFn: echoCreate}
		est := &stubEstimator{quote: money.FromUSD(1645000)}

		resp, err := grpcserver.New(repo, est).CreateVoyage(context.Background(), validCreateReq())

		require.NoError(t, err)
		assert.False(t, resp.GetEstimatePending())
		assert.Equal(t, int64(1645000), resp.GetProvisionalEstimateMinor())
		assert.Equal(t, int64(1), resp.GetVoyage().GetVersion())
	})
}

func TestUpdateVoyage(t *testing.T) {
	validReq := &voyagev1.UpdateVoyageRequest{Voyage: &voyagev1.Voyage{
		ClientRequestId: "req-1", Origin: "NLRTM", Dest: "SGSIN", DistanceNm: 8200, FeesMinor: 5000, Version: 2,
	}}

	t.Run("rejects invalid input with InvalidArgument", func(t *testing.T) {
		repo := &stubRepo{updateFn: func(v domain.Voyage) (domain.Voyage, error) { return v, nil }}
		req := &voyagev1.UpdateVoyageRequest{Voyage: &voyagev1.Voyage{ClientRequestId: ""}}

		_, err := grpcserver.New(repo, &stubEstimator{}).UpdateVoyage(context.Background(), req)

		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("maps ErrVoyageNotFound to NotFound", func(t *testing.T) {
		repo := &stubRepo{updateFn: func(domain.Voyage) (domain.Voyage, error) {
			return domain.Voyage{}, domain.ErrVoyageNotFound
		}}

		_, err := grpcserver.New(repo, &stubEstimator{}).UpdateVoyage(context.Background(), validReq)

		assert.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("happy path returns the current persisted voyage", func(t *testing.T) {
		repo := &stubRepo{updateFn: func(v domain.Voyage) (domain.Voyage, error) { return v, nil }}

		resp, err := grpcserver.New(repo, &stubEstimator{}).UpdateVoyage(context.Background(), validReq)

		require.NoError(t, err)
		assert.Equal(t, int64(2), resp.GetVoyage().GetVersion())
	})
}

func TestGetVoyage(t *testing.T) {
	t.Run("empty id is InvalidArgument", func(t *testing.T) {
		_, err := grpcserver.New(&stubRepo{}, &stubEstimator{}).
			GetVoyage(context.Background(), &voyagev1.GetVoyageRequest{ClientRequestId: ""})

		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("unknown voyage is NotFound", func(t *testing.T) {
		repo := &stubRepo{getFn: func(string) (domain.Voyage, error) {
			return domain.Voyage{}, domain.ErrVoyageNotFound
		}}

		_, err := grpcserver.New(repo, &stubEstimator{}).
			GetVoyage(context.Background(), &voyagev1.GetVoyageRequest{ClientRequestId: "nope"})

		assert.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("happy path returns the voyage", func(t *testing.T) {
		repo := &stubRepo{getFn: func(id string) (domain.Voyage, error) {
			return domain.Voyage{ClientRequestID: id, Origin: "NLRTM", Dest: "SGSIN", Version: 3, EstimateMinor: 1645000}, nil
		}}

		resp, err := grpcserver.New(repo, &stubEstimator{}).
			GetVoyage(context.Background(), &voyagev1.GetVoyageRequest{ClientRequestId: "req-1"})

		require.NoError(t, err)
		assert.Equal(t, int64(1645000), resp.GetVoyage().GetEstimateMinor())
	})
}
