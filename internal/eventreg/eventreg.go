// Package eventreg is the single list of Avro event registrations both services
// load into their serde — voyage.created and estimate.ready. Keeping it in one
// place means a new event is registered everywhere at once.
package eventreg

import (
	"maritime-test-lab/gen/avro"
	"maritime-test-lab/internal/avroserde"
	"maritime-test-lab/schemas"
)

// All returns the registrations for every event in the lab.
func All() []avroserde.Registration {
	return []avroserde.Registration{
		{Subject: schemas.VoyageCreatedSubject, Schema: schemas.VoyageCreated, Example: avro.VoyageCreated{}},
		{Subject: schemas.EstimateReadySubject, Schema: schemas.EstimateReady, Example: avro.EstimateReady{}},
	}
}
