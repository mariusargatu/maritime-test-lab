package domain_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"maritime-test-lab/services/voyage/domain"
)

func TestVoyageValidate(t *testing.T) {
	valid := domain.Voyage{ClientRequestID: "req-1", Origin: "NLRTM", Dest: "SGSIN", DistanceNm: 8200}

	tests := []struct {
		name    string
		mutate  func(domain.Voyage) domain.Voyage // derives the case from a copy of valid
		wantErr error
	}{
		{name: "accepts a complete voyage", mutate: func(v domain.Voyage) domain.Voyage { return v }},
		{name: "rejects missing client_request_id", mutate: func(v domain.Voyage) domain.Voyage { v.ClientRequestID = ""; return v }, wantErr: domain.ErrMissingClientRequestID},
		{name: "rejects missing origin", mutate: func(v domain.Voyage) domain.Voyage { v.Origin = ""; return v }, wantErr: domain.ErrMissingPort},
		{name: "rejects missing dest", mutate: func(v domain.Voyage) domain.Voyage { v.Dest = ""; return v }, wantErr: domain.ErrMissingPort},
		{name: "rejects negative distance", mutate: func(v domain.Voyage) domain.Voyage { v.DistanceNm = -1; return v }, wantErr: domain.ErrNegativeDistance},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.mutate(valid).Validate()

			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}
