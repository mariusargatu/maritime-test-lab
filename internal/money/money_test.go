package money_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"maritime-test-lab/internal/money"
)

func TestAdd(t *testing.T) {
	tests := []struct {
		name    string
		a, b    money.Money
		want    money.Money
		wantErr error
	}{
		{name: "sums same currency", a: money.FromUSD(250), b: money.FromUSD(50), want: money.FromUSD(300)},
		{name: "adds zero", a: money.FromUSD(100), b: money.FromUSD(0), want: money.FromUSD(100)},
		{name: "rejects currency mismatch", a: money.FromUSD(1), b: money.Money{Minor: 1, Currency: "EUR"}, wantErr: money.ErrCurrencyMismatch},
		{name: "rejects overflow", a: money.FromUSD(math.MaxInt64), b: money.FromUSD(1), wantErr: money.ErrOverflow},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.a.Add(tc.b)

			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestMul(t *testing.T) {
	tests := []struct {
		name    string
		m       money.Money
		factor  int64
		want    money.Money
		wantErr error
	}{
		{name: "scales by factor", m: money.FromUSD(200), factor: 3, want: money.FromUSD(600)},
		{name: "zero factor is zero", m: money.FromUSD(200), factor: 0, want: money.FromUSD(0)},
		{name: "rejects overflow", m: money.FromUSD(math.MaxInt64), factor: 2, wantErr: money.ErrOverflow},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.m.Mul(tc.factor)

			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestAddDoesNotMutate documents the value-semantics contract: Add returns a new
// Money and leaves the operands untouched.
func TestAddDoesNotMutate(t *testing.T) {
	a := money.FromUSD(100)
	b := money.FromUSD(50)

	_, err := a.Add(b)

	require.NoError(t, err)
	assert.Equal(t, money.FromUSD(100), a)
	assert.Equal(t, money.FromUSD(50), b)
}
