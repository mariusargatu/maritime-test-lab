package schemacheck

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/reflect/protoreflect"

	"maritime-test-lab/gen/avro"
	estimatorv1 "maritime-test-lab/gen/proto/estimator/v1"
	voyagev1 "maritime-test-lab/gen/proto/voyage/v1"
)

// Parity catches cross-representation drift: a field added to the proto (and SQL,
// and GraphQL) but forgotten on the event, or vice versa. Each pair carries an
// explicit allowlist of fields that legitimately live on only one side — adding
// to the allowlist is the conscious decision the gate records.

func TestVoyageCreatedParity(t *testing.T) {
	allow := map[string]bool{
		// estimate_minor is populated asynchronously, after creation — so it is on
		// the proto/SQL but not on the voyage.created event.
		"estimate_minor": true,
	}
	proto := minus(protoFieldNames((&voyagev1.Voyage{}).ProtoReflect()), allow)
	event := minus(avroFieldNames(avro.VoyageCreated{}), allow)
	assert.Equal(t, proto, event, "voyage.v1.Voyage vs voyage_created.avsc field names diverged")
}

func TestEstimateReadyParity(t *testing.T) {
	allow := map[string]bool{
		// event-only correlation fields, absent from the sync EstimateResponse.
		"voyage_id":      true,
		"voyage_version": true,
		"calculated_at":  true,
	}
	proto := minus(protoFieldNames((&estimatorv1.EstimateResponse{}).ProtoReflect()), allow)
	event := minus(avroFieldNames(avro.EstimateReady{}), allow)
	assert.Equal(t, proto, event, "estimator.v1.EstimateResponse vs estimate_ready.avsc money fields diverged")
}

func protoFieldNames(m protoreflect.Message) map[string]bool {
	fields := m.Descriptor().Fields()
	names := make(map[string]bool, fields.Len())
	for i := 0; i < fields.Len(); i++ {
		names[string(fields.Get(i).Name())] = true
	}
	return names
}

func avroFieldNames(v any) map[string]bool {
	t := reflect.TypeOf(v)
	names := make(map[string]bool, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		if tag := t.Field(i).Tag.Get("avro"); tag != "" {
			names[tag] = true
		}
	}
	return names
}

func minus(set, remove map[string]bool) map[string]bool {
	out := make(map[string]bool, len(set))
	for k := range set {
		if !remove[k] {
			out[k] = true
		}
	}
	return out
}
