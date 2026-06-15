// Package schemas embeds the Avro event schemas (the .avsc files in this
// directory) so the running services register exactly the committed text — the
// same files the codegen, the parity test, and the SR compat gate use. One
// source of truth for each event.
package schemas

import _ "embed"

// Topic names (and their dead-letter siblings).
const (
	TopicVoyageCreated    = "voyage.created"
	TopicEstimateReady    = "estimate.ready"
	TopicVoyageCreatedDLQ = "voyage.created.dlq"
	TopicEstimateReadyDLQ = "estimate.ready.dlq"
)

// Schema Registry subjects (TopicNameStrategy: <topic>-value).
const (
	VoyageCreatedSubject = TopicVoyageCreated + "-value"
	EstimateReadySubject = TopicEstimateReady + "-value"
)

// Schema texts, embedded from the committed .avsc files.
var (
	//go:embed voyage_created.avsc
	VoyageCreated string

	//go:embed estimate_ready.avsc
	EstimateReady string
)
