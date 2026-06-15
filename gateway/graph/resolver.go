package graph

import voyagev1 "maritime-test-lab/gen/proto/voyage/v1"

// Resolver is the GraphQL dependency-injection root. The gateway talks ONLY to
// the voyage service (D-004), so its single dependency is the voyage gRPC client.
type Resolver struct {
	// VoyageClient — not "Voyage": that would collide with the Voyage query
	// resolver method and shadow the field.
	VoyageClient voyagev1.VoyageServiceClient
}
