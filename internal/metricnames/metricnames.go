// Package metricnames is the single source of truth for Prometheus metric names.
// The services emit under these consts and the estimate-pipeline monitor's alert
// rules reference the same strings — a guard test asserts the rule files use
// them, so renaming a const without updating the rules fails the gate (D-046).
package metricnames

const (
	// CreateDedupe counts idempotent CreateVoyage replays.
	CreateDedupe = "voyage_create_dedupe_total"
	// EstimateApplied counts estimate.ready events applied — the pipeline throughput.
	EstimateApplied = "voyage_estimate_applied_total"
	// EstimateErrors counts estimate processing failures.
	EstimateErrors = "voyage_estimate_errors_total"
	// EstimateLatency is the create→estimate end-to-end latency histogram (seconds).
	EstimateLatency = "voyage_estimate_latency_seconds"
	// DLQDepth is the dead-letter-queue depth gauge.
	DLQDepth = "voyage_dlq_depth"
	// ConsumerLag is the Kafka consumer-lag gauge (dashboard surface; the perf gate
	// computes lag via kadm.Lag instead, D-032).
	ConsumerLag = "voyage_consumer_lag"
)
