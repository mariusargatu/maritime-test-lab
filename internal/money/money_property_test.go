package money_test

import (
	"testing"

	"pgregory.net/rapid"

	"maritime-test-lab/internal/money"
)

// Property tests for Money's algebra, beside the golden table in money_test.go.
// Ranges stay well inside int64 so valid inputs never legitimately overflow — the
// overflow path itself is covered by the table. Seeded for deterministic replay.

func TestAdd_Commutative(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		x := rapid.Int64Range(-1_000_000_000, 1_000_000_000).Draw(t, "x")
		y := rapid.Int64Range(-1_000_000_000, 1_000_000_000).Draw(t, "y")

		xy, err := money.FromUSD(x).Add(money.FromUSD(y))
		if err != nil {
			t.Fatalf("x+y errored: %v", err)
		}
		yx, err := money.FromUSD(y).Add(money.FromUSD(x))
		if err != nil {
			t.Fatalf("y+x errored: %v", err)
		}
		if xy != yx {
			t.Fatalf("a+b != b+a: %+v vs %+v", xy, yx)
		}
	})
}

func TestAdd_ZeroIsIdentity(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		x := rapid.Int64Range(-1_000_000_000_000, 1_000_000_000_000).Draw(t, "x")
		m := money.FromUSD(x)

		got, err := m.Add(money.FromUSD(0))
		if err != nil {
			t.Fatalf("add zero errored: %v", err)
		}
		if got != m {
			t.Fatalf("m + 0 != m: %+v vs %+v", got, m)
		}
	})
}

func TestMul_NonNegativeStaysNonNegative(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		minor := rapid.Int64Range(0, 1_000_000).Draw(t, "minor")
		factor := rapid.Int64Range(0, 1_000_000).Draw(t, "factor")

		got, err := money.FromUSD(minor).Mul(factor)
		if err != nil {
			t.Fatalf("mul errored: %v", err)
		}
		if got.Minor < 0 {
			t.Fatalf("non-negative * non-negative is negative: %d", got.Minor)
		}
		if got.Currency != money.USD {
			t.Fatalf("currency not preserved: %q", got.Currency)
		}
	})
}
