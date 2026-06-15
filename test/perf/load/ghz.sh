#!/usr/bin/env bash
# ghz load against the estimator's sync Estimate gRPC (Go-native gRPC load tool,
# D-048). Demo only — the perf GATE is the Go lag-drain test (make perf). Needs
# the edge stack up and ghz in ./bin (make bootstrap).
set -euo pipefail
cd "$(dirname "$0")/../../.."

ADDR="${ESTIMATOR_ADDR:-localhost:9001}"
./bin/ghz --insecure \
  --proto proto/estimator/v1/estimator.proto \
  --call estimator.v1.EstimatorService.Estimate \
  -d '{"client_request_id":"load-{{.RequestNumber}}","distance_nm":8200,"fees_minor":5000}' \
  --rps 50 --duration 30s \
  "$ADDR"
