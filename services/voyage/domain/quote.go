package domain

import (
	"context"
	"fmt"

	"maritime-test-lab/internal/money"
)

// Quote validates a voyage and asks the Estimator for a provisional cost. It is
// the smallest use-case that exercises a port (the north-star teaching example);
// the production create flow lives in grpcserver.CreateVoyage, which validates
// then calls the Estimator directly and does not use Quote.
func Quote(ctx context.Context, v Voyage, est Estimator) (money.Money, error) {
	if err := v.Validate(); err != nil {
		return money.Money{}, fmt.Errorf("quote: %w", err)
	}
	q, err := est.Estimate(ctx, v)
	if err != nil {
		return money.Money{}, fmt.Errorf("quote %s: %w", v.ClientRequestID, err)
	}
	return q, nil
}
