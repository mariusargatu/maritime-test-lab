package domain_test

import (
	"testing"

	"pgregory.net/rapid"

	"maritime-test-lab/internal/money"
	"maritime-test-lab/services/estimator/domain"
)

// Property tests are the sanctioned exception to the "no randomness in tests"
// rule (CLAUDE.md): rapid replays deterministically and shrinks any failure to a
// minimal counterexample. Each asserts one named invariant of EstimateCost.
// Input ranges stay well inside int64 so valid inputs never legitimately overflow.

func TestEstimateCost_NeverNegative(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		distance := rapid.Int32Range(0, 1_000_000).Draw(t, "distance")
		fees := rapid.Int64Range(0, 1_000_000_000).Draw(t, "fees")
		rate := rapid.Int64Range(1, 1_000_000).Draw(t, "rate")

		got, err := domain.EstimateCost(distance, money.FromUSD(fees), money.FromUSD(rate))
		if err != nil {
			t.Fatalf("valid inputs errored: %v", err)
		}
		if got.Minor < 0 {
			t.Fatalf("cost is negative: %d", got.Minor)
		}
	})
}

func TestEstimateCost_MonotonicInDistance(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		base := rapid.Int32Range(0, 500_000).Draw(t, "base")
		delta := rapid.Int32Range(0, 500_000).Draw(t, "delta")
		fees := rapid.Int64Range(0, 1_000_000).Draw(t, "fees")
		rate := rapid.Int64Range(1, 1_000).Draw(t, "rate")

		lo, err := domain.EstimateCost(base, money.FromUSD(fees), money.FromUSD(rate))
		if err != nil {
			t.Fatalf("lo errored: %v", err)
		}
		hi, err := domain.EstimateCost(base+delta, money.FromUSD(fees), money.FromUSD(rate))
		if err != nil {
			t.Fatalf("hi errored: %v", err)
		}
		if hi.Minor < lo.Minor {
			t.Fatalf("more distance cost less: %d nm gave %d, %d nm gave %d", base, lo.Minor, base+delta, hi.Minor)
		}
	})
}

func TestEstimateCost_PreservesCurrency(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		distance := rapid.Int32Range(0, 1_000_000).Draw(t, "distance")
		fees := rapid.Int64Range(0, 1_000_000).Draw(t, "fees")
		rate := rapid.Int64Range(1, 1_000).Draw(t, "rate")

		got, err := domain.EstimateCost(distance, money.FromUSD(fees), money.FromUSD(rate))
		if err != nil {
			t.Fatalf("errored: %v", err)
		}
		if got.Currency != money.USD {
			t.Fatalf("currency not preserved: %q", got.Currency)
		}
	})
}
