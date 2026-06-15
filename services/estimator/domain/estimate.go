// Package domain holds the estimator's pricing logic. It is pure: inputs in,
// value out, no side effects and no infrastructure imports, so the cost calc is
// tested with a golden dataset and seeded property tests (no I/O).
package domain

import (
	"errors"
	"fmt"

	"maritime-test-lab/internal/money"
)

// Sentinel errors from the cost calculation. Callers match with errors.Is.
var (
	// ErrNegativeDistance is returned for a distance below zero.
	ErrNegativeDistance = errors.New("estimate: negative distance")
	// ErrNonPositiveRate is returned for a rate that is zero or negative.
	ErrNonPositiveRate = errors.New("estimate: rate must be positive")
)

// EstimateCost prices a voyage leg as distance * ratePerNm + fees, returning a
// new Money. Bad inputs are rejected rather than silently coerced (D-038): a
// negative distance, a non-positive rate, or an int64 overflow each return a
// sentinel error. fees and ratePerNm must share a currency.
func EstimateCost(distanceNm int32, fees money.Money, ratePerNm money.Money) (money.Money, error) {
	if distanceNm < 0 {
		return money.Money{}, fmt.Errorf("distance %d: %w", distanceNm, ErrNegativeDistance)
	}
	if ratePerNm.Minor <= 0 {
		return money.Money{}, fmt.Errorf("rate %d: %w", ratePerNm.Minor, ErrNonPositiveRate)
	}

	base, err := ratePerNm.Mul(int64(distanceNm))
	if err != nil {
		return money.Money{}, fmt.Errorf("estimate: %w", err)
	}
	total, err := base.Add(fees)
	if err != nil {
		return money.Money{}, fmt.Errorf("estimate: %w", err)
	}
	return total, nil
}
