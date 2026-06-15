// Package pact holds the L3 consumer-driven contract tests (build tag `pact`):
// the voyage→estimator gRPC pact (pact-go + protobuf plugin) and the async
// message pacts for voyage.created and estimate.ready. Brokerless: consumer tests
// write pact files to ./pacts and the provider tests verify them in the same
// run (D-009). These tests need the pact FFI — see make contract.
package pact
