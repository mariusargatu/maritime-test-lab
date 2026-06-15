// Package money is a pure value type for monetary amounts: integer minor units
// (e.g. USD cents) plus a currency code, never float. It is shared by the
// estimator's cost calc and voyage's Estimator port, so it lives here rather
// than inside either service's domain. No infrastructure imports.
package money

import (
	"errors"
	"fmt"
)

// USD is the only currency in v1.
const USD = "USD"

// Sentinel errors. Callers match with errors.Is.
var (
	// ErrOverflow is returned when an operation would exceed int64 range.
	ErrOverflow = errors.New("money: arithmetic overflow")
	// ErrCurrencyMismatch is returned when two amounts have different currencies.
	ErrCurrencyMismatch = errors.New("money: currency mismatch")
)

// Money is an amount in integer minor units plus an ISO 4217 currency code.
// Treat it as immutable: Add and Mul return new values and never mutate the
// receiver.
type Money struct {
	Minor    int64
	Currency string
}

// FromUSD returns minor USD units as Money.
func FromUSD(minor int64) Money {
	return Money{Minor: minor, Currency: USD}
}

// Add returns the sum of m and other. It errors on a currency mismatch or if
// the result would overflow int64.
func (m Money) Add(other Money) (Money, error) {
	if m.Currency != other.Currency {
		return Money{}, fmt.Errorf("add %s+%s: %w", m.Currency, other.Currency, ErrCurrencyMismatch)
	}
	sum := m.Minor + other.Minor
	// Signed overflow: adding a positive that shrinks the result (or a negative
	// that grows it) means the int64 range was exceeded.
	if (other.Minor > 0 && sum < m.Minor) || (other.Minor < 0 && sum > m.Minor) {
		return Money{}, fmt.Errorf("add %d+%d: %w", m.Minor, other.Minor, ErrOverflow)
	}
	return Money{Minor: sum, Currency: m.Currency}, nil
}

// Mul returns m scaled by factor. It errors if the result would overflow int64.
func (m Money) Mul(factor int64) (Money, error) {
	if m.Minor == 0 || factor == 0 {
		return Money{Minor: 0, Currency: m.Currency}, nil
	}
	product := m.Minor * factor
	// Verify by dividing back: if it doesn't round-trip, the multiply overflowed.
	if product/factor != m.Minor {
		return Money{}, fmt.Errorf("mul %d*%d: %w", m.Minor, factor, ErrOverflow)
	}
	return Money{Minor: product, Currency: m.Currency}, nil
}
