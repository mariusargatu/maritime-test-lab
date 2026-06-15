// Package schemacheck holds the Go-level schema-correctness guards for the event
// contracts — the cheap, no-I/O complements to the Schema Registry FULL-compat
// gate in test/contract. Two kinds:
//
//   - parity (cross-representation): the field names of a proto message and its
//     corresponding Avro event must stay in sync, minus an explicit allowlist —
//     catches "added a field to proto/SQL, forgot the event" (or vice versa).
//   - evolution (cross-version): a value encoded with a GENUINE prior schema
//     (schemas/history/*.v1 — each carries a since-removed field with a default)
//     must round-trip through the current schema and back, exercising the actual
//     bytes the registry's algebraic compatibility check never moves. It ships a
//     red self-test: a type-incompatible historical schema must be rejected, so
//     the gate is known to be able to fail.
//
// Both are L1: pure reflection / in-memory marshal, no containers.
package schemacheck
