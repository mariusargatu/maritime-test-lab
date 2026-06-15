// Package perf holds the L6 perf gate (build tag `perf`, run by `make perf`). It
// gates only on noise-immune signals — error rate and consumer-lag drain — never
// on latency (which is trend-only on shared infra, D-019). Lag is read via
// franz-go's kadm admin API directly from the broker, so the gate needs no
// Prometheus/obs containers (D-032).
package perf
