package domain_test

import (
	"encoding/csv"
	"os"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"maritime-test-lab/internal/money"
	"maritime-test-lab/services/estimator/domain"
)

// The golden dataset lives in testdata/golden.csv (D-038). The loader only reads
// values — expected results are literal in the file, never computed here — so the
// "no logic in tests" rule holds.
type goldenRow struct {
	name     string
	distance int32
	fees     int64
	rate     int64
	want     int64
	wantErr  error
}

func TestEstimateCost_Golden(t *testing.T) {
	for _, tc := range loadGolden(t) {
		t.Run(tc.name, func(t *testing.T) {
			got, err := domain.EstimateCost(tc.distance, money.FromUSD(tc.fees), money.FromUSD(tc.rate))

			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, money.FromUSD(tc.want), got)
		})
	}
}

func loadGolden(t *testing.T) []goldenRow {
	t.Helper()

	f, err := os.Open("testdata/golden.csv")
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()

	records, err := csv.NewReader(f).ReadAll()
	require.NoError(t, err)
	require.Greater(t, len(records), 1, "golden.csv needs a header plus rows")

	rows := make([]goldenRow, 0, len(records)-1)
	for _, rec := range records[1:] { // skip header
		rows = append(rows, goldenRow{
			name:     rec[0],
			distance: int32(mustInt(t, rec[1])),
			fees:     mustInt(t, rec[2]),
			rate:     mustInt(t, rec[3]),
			want:     mustInt(t, rec[4]),
			wantErr:  errByName(t, rec[5]),
		})
	}
	return rows
}

func mustInt(t *testing.T, s string) int64 {
	t.Helper()
	n, err := strconv.ParseInt(s, 10, 64)
	require.NoErrorf(t, err, "parse %q as int", s)
	return n
}

func errByName(t *testing.T, name string) error {
	t.Helper()
	switch name {
	case "":
		return nil
	case "ErrNegativeDistance":
		return domain.ErrNegativeDistance
	case "ErrNonPositiveRate":
		return domain.ErrNonPositiveRate
	case "ErrOverflow":
		return money.ErrOverflow
	default:
		t.Fatalf("unknown error name %q in golden.csv", name)
		return nil
	}
}
