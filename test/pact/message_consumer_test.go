//go:build pact

package pact_test

import (
	"testing"

	"github.com/pact-foundation/pact-go/v2/matchers"
	message "github.com/pact-foundation/pact-go/v2/message/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"maritime-test-lab/internal/money"
	estimatordomain "maritime-test-lab/services/estimator/domain"
)

// Message pact — estimator consumes voyage.created (provider = voyage). The
// consumer states only the fields it needs; ConsumedBy proves it can price them.
func TestVoyageCreatedMessageConsumer(t *testing.T) {
	p, err := message.NewAsynchronousPact(message.Config{
		Consumer: "estimator",
		Provider: "voyage",
		PactDir:  pactDir(t),
	})
	require.NoError(t, err)

	err = p.AddAsynchronousMessage().
		Given("a voyage was created").
		ExpectsToReceive("a voyage.created event").
		WithJSONContent(matchers.Map{
			"client_request_id": matchers.Like("11111111-1111-1111-1111-111111111111"),
			"origin":            matchers.Like("NLRTM"),
			"dest":              matchers.Like("SGSIN"),
			"distance_nm":       matchers.Integer(8200),
			"fees_minor":        matchers.Integer(5000),
			"version":           matchers.Integer(1),
		}).
		AsType(&voyageCreatedDTO{}).
		ConsumedBy(func(mc message.AsynchronousMessage) error {
			vc := mc.Body.(*voyageCreatedDTO)
			cost, err := estimatordomain.EstimateCost(vc.DistanceNm, money.FromUSD(vc.FeesMinor), money.FromUSD(200))
			require.NoError(t, err)
			assert.Positive(t, cost.Minor)
			return nil
		}).
		Verify(t)
	require.NoError(t, err)
}

// Message pact — voyage consumes estimate.ready (provider = estimator). Merges
// into voyage-estimator.json alongside the gRPC interaction (same pair).
func TestEstimateReadyMessageConsumer(t *testing.T) {
	p, err := message.NewAsynchronousPact(message.Config{
		Consumer: "voyage",
		Provider: "estimator",
		PactDir:  pactDir(t),
	})
	require.NoError(t, err)

	err = p.AddAsynchronousMessage().
		Given("a voyage was priced").
		ExpectsToReceive("an estimate.ready event").
		WithJSONContent(matchers.Map{
			"voyage_id":      matchers.Like("11111111-1111-1111-1111-111111111111"),
			"voyage_version": matchers.Integer(1),
			"amount_minor":   matchers.Integer(1645000),
			"currency":       matchers.Like("USD"),
		}).
		AsType(&estimateReadyDTO{}).
		ConsumedBy(func(mc message.AsynchronousMessage) error {
			er := mc.Body.(*estimateReadyDTO)
			// the voyage consumer needs an id, a version, and an amount to upsert
			assert.NotEmpty(t, er.VoyageID)
			assert.Positive(t, er.VoyageVersion)
			assert.Positive(t, er.AmountMinor)
			return nil
		}).
		Verify(t)
	require.NoError(t, err)
}
