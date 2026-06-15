// Package domain is the onBOARD sync client's core: the offline queue logic,
// retry policy, version-counter merge, and the ports it needs (Store, Transport,
// Clock). It imports no infrastructure — no http, no os, no grpc — so it tests
// with fakes and zero I/O. The library is driven in-process by the L5 resilience
// tests (D-017).
package domain

// Kind is the kind of voyage mutation queued for sync.
type Kind string

const (
	KindCreate Kind = "create"
	KindUpdate Kind = "update"
)

// Operation is one voyage mutation awaiting sync to the gateway. It is keyed by
// ClientRequestID; a newer Operation for the same id supersedes the older
// (higher Version wins).
type Operation struct {
	ClientRequestID string `json:"client_request_id"`
	Kind            Kind   `json:"kind"`
	Origin          string `json:"origin"`
	Dest            string `json:"dest"`
	DistanceNm      int32  `json:"distance_nm"`
	FeesMinor       int64  `json:"fees_minor"`
	Version         int64  `json:"version"`
}
