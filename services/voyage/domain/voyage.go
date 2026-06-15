// Package domain holds the voyage service's business types, validation, and the
// ports it depends on (VoyageRepository, EventPublisher, Estimator). It imports
// no infrastructure: no grpc, sql, or kafka types, which is what lets the logic
// tests run with hand-written fakes and zero I/O.
package domain

import (
	"errors"
	"fmt"
)

// Validation sentinels. The gRPC handler validates at the boundary and returns
// clear messages; callers match these with errors.Is.
var (
	ErrMissingClientRequestID = errors.New("voyage: client_request_id required")
	ErrMissingPort            = errors.New("voyage: origin and dest required")
	ErrNegativeDistance       = errors.New("voyage: negative distance")
	// ErrVoyageNotFound is returned by VoyageRepository.Get for an unknown id.
	ErrVoyageNotFound = errors.New("voyage: not found")
)

// Voyage is a planned sailing between two ports. Treat it as a value: produce a
// new Voyage rather than mutating an existing one (the version counter is how
// concurrent edits are resolved, so in-place mutation would hide conflicts).
type Voyage struct {
	ClientRequestID string // caller-supplied UUID; the idempotency key
	Origin          string // UN/LOCODE, e.g. "NLRTM"
	Dest            string // UN/LOCODE, e.g. "SGSIN"
	DistanceNm      int32  // nautical miles, client-supplied
	FeesMinor       int64  // flat fees, USD minor units (cents)
	Version         int64  // monotonic per-record counter; higher wins on conflict
	EstimateMinor   int64  // authoritative estimate, async-populated (P3); 0 = pending
}

// Validate reports the first reason the voyage cannot be accepted, or nil. The
// domain trusts its inputs once this passes.
func (v Voyage) Validate() error {
	if v.ClientRequestID == "" {
		return ErrMissingClientRequestID
	}
	if v.Origin == "" || v.Dest == "" {
		return ErrMissingPort
	}
	if v.DistanceNm < 0 {
		return fmt.Errorf("distance %d: %w", v.DistanceNm, ErrNegativeDistance)
	}
	return nil
}
