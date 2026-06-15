package graph

import (
	"maritime-test-lab/gen/graphql/model"
	voyagev1 "maritime-test-lab/gen/proto/voyage/v1"
)

// toModelVoyage maps a proto voyage to the GraphQL model. The int64/int32 proto
// fields become Go int (GraphQL Int) — lab money values fit comfortably.
func toModelVoyage(v *voyagev1.Voyage) *model.Voyage {
	return &model.Voyage{
		ClientRequestID: v.GetClientRequestId(),
		Origin:          v.GetOrigin(),
		Dest:            v.GetDest(),
		DistanceNm:      int(v.GetDistanceNm()),
		FeesMinor:       int(v.GetFeesMinor()),
		Version:         int(v.GetVersion()),
		EstimateMinor:   int(v.GetEstimateMinor()),
	}
}
