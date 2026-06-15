#!/usr/bin/env bash
# vegeta load against the GraphQL gateway's createVoyage (Go-native HTTP load
# tool, D-048 — the dominant Go-native HTTP load tool). Demo only; the
# perf GATE is the Go test (make perf). Generates a JSONL targets file with unique
# client_request_ids, then attacks. Needs the edge stack up + vegeta in ./bin.
set -euo pipefail
cd "$(dirname "$0")/../../.."

URL="${GATEWAY_URL:-http://localhost:18080/query}"
N="${N:-600}"

targets="$(mktemp)"
trap 'rm -f "$targets"' EXIT

query='mutation($i:CreateVoyageInput!){createVoyage(input:$i){voyage{clientRequestId}}}'
for _ in $(seq 1 "$N"); do
  id="$(uuidgen)"
  body=$(printf '{"query":"%s","variables":{"i":{"clientRequestId":"%s","origin":"NLRTM","dest":"SGSIN","distanceNm":8200,"feesMinor":5000}}}' "$query" "$id")
  b64=$(printf '%s' "$body" | base64)
  printf '{"method":"POST","url":"%s","body":"%s","header":{"Content-Type":["application/json"]}}\n' "$URL" "$b64" >> "$targets"
done

./bin/vegeta attack -format=json -targets="$targets" -rate=20 -duration=30s | ./bin/vegeta report
