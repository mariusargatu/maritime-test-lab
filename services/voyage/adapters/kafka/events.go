// Package kafka is the voyage service's event adapter: the outbox poller that
// publishes voyage.created, and (from the consumer side) applies estimate.ready.
// It owns the Avro/SR serde so the domain and the repository stay wire-free.
package kafka

import (
	"maritime-test-lab/gen/avro"
	"maritime-test-lab/internal/avroserde"
	"maritime-test-lab/schemas"
	"maritime-test-lab/services/voyage/domain"
)

// VoyageCreatedEncoder returns an outbox encoder that serializes a voyage as a
// voyage.created event. The return type matches postgres.OutboxEncoder
// structurally, so the repository never imports the serde.
func VoyageCreatedEncoder(serde *avroserde.Serde) func(domain.Voyage) (string, []byte, error) {
	return func(v domain.Voyage) (string, []byte, error) {
		payload, err := serde.Encode(ToVoyageCreated(v))
		if err != nil {
			return "", nil, err
		}
		return schemas.TopicVoyageCreated, payload, nil
	}
}

// ToVoyageCreated maps a voyage to its voyage.created event. Exported so the
// message-pact provider verify checks the real builder, not a copy.
func ToVoyageCreated(v domain.Voyage) avro.VoyageCreated {
	return avro.VoyageCreated{
		ClientRequestID: v.ClientRequestID,
		Origin:          v.Origin,
		Dest:            v.Dest,
		DistanceNm:      v.DistanceNm,
		FeesMinor:       v.FeesMinor,
		Version:         v.Version,
	}
}
