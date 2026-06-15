package domain_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"maritime-test-lab/internal/money"
	"maritime-test-lab/services/voyage/domain"
)

// Hand-written fakes for the three voyage ports (preferred over generated mocks
// for domain tests — CLAUDE.md). The Estimator fake is exercised by Quote below;
// the repository and publisher are minimal interface-satisfying stubs here and
// become stateful in the Phase 2 flow that actually drives them.

type fakeEstimator struct {
	quote money.Money
	err   error
	calls int
}

func (f *fakeEstimator) Estimate(context.Context, domain.Voyage) (money.Money, error) {
	f.calls++
	return f.quote, f.err
}

type fakeVoyageRepository struct{}

func (fakeVoyageRepository) Create(_ context.Context, v domain.Voyage) (domain.Voyage, error) {
	return v, nil
}
func (fakeVoyageRepository) Update(_ context.Context, v domain.Voyage) (domain.Voyage, error) {
	return v, nil
}
func (fakeVoyageRepository) Get(context.Context, string) (domain.Voyage, error) {
	return domain.Voyage{}, nil
}

type fakeEventPublisher struct{}

func (fakeEventPublisher) PublishVoyageCreated(context.Context, domain.Voyage) error { return nil }

// Compile-time proof the fakes satisfy the ports — keeps them in lockstep as the
// interfaces evolve.
var (
	_ domain.Estimator        = (*fakeEstimator)(nil)
	_ domain.VoyageRepository = fakeVoyageRepository{}
	_ domain.EventPublisher   = fakeEventPublisher{}
)

func TestQuote(t *testing.T) {
	valid := domain.Voyage{ClientRequestID: "req-1", Origin: "NLRTM", Dest: "SGSIN", DistanceNm: 8200}

	t.Run("returns the estimator quote for a valid voyage", func(t *testing.T) {
		est := &fakeEstimator{quote: money.FromUSD(25000)}

		got, err := domain.Quote(context.Background(), valid, est)

		require.NoError(t, err)
		assert.Equal(t, money.FromUSD(25000), got)
		assert.Equal(t, 1, est.calls)
	})

	t.Run("rejects an invalid voyage before calling the estimator", func(t *testing.T) {
		est := &fakeEstimator{quote: money.FromUSD(1)}
		invalid := valid
		invalid.ClientRequestID = ""

		_, err := domain.Quote(context.Background(), invalid, est)

		require.ErrorIs(t, err, domain.ErrMissingClientRequestID)
		assert.Equal(t, 0, est.calls, "estimator must not be called for invalid input")
	})
}
