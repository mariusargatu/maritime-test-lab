package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"maritime-test-lab/clients/onboard-sync/domain"
)

func TestHigherVersionWins(t *testing.T) {
	op := func(version, fees int64) domain.Operation {
		return domain.Operation{ClientRequestID: "x", Version: version, FeesMinor: fees}
	}

	tests := []struct {
		name     string
		current  domain.Operation
		incoming domain.Operation
		wantFees int64
	}{
		{name: "incoming higher version wins", current: op(1, 100), incoming: op(2, 200), wantFees: 200},
		{name: "incoming lower version keeps current", current: op(3, 300), incoming: op(1, 999), wantFees: 300},
		{name: "tie keeps current (idempotent)", current: op(2, 222), incoming: op(2, 999), wantFees: 222},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := domain.HigherVersionWins(tc.current, tc.incoming)

			assert.Equal(t, tc.wantFees, got.FeesMinor)
		})
	}
}
