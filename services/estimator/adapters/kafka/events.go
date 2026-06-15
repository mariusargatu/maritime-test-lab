package kafka

import (
	"time"

	"maritime-test-lab/gen/avro"
	"maritime-test-lab/internal/money"
)

// BuildEstimateReady maps a priced voyage.created into an estimate.ready event.
// Exported so the message-pact provider verify checks the real builder.
func BuildEstimateReady(vc avro.VoyageCreated, cost money.Money, at time.Time) avro.EstimateReady {
	return avro.EstimateReady{
		VoyageID:      vc.ClientRequestID,
		VoyageVersion: vc.Version,
		AmountMinor:   cost.Minor,
		Currency:      cost.Currency,
		CalculatedAt:  at.UTC(),
	}
}
