# maritime-test-lab

A small, **deliberately readable** reference system that reproduces a production microservices
testing stack — and wires the **full test pyramid** end-to-end so you can *see how the layers connect*.

Not a product. A worked example: 2 Go gRPC services + Kafka/Avro + Postgres + a GraphQL gateway,
wrapped in static → unit → service-integration → contract → whole-stack smoke → resilience → perf →
shift-right monitoring. The toy domain is a **voyage cost estimator** (a money-calc analog) — the
interesting part is the *testing*, not the maritime logic.

> **Why it exists:** to learn how the test layers connect, and to be something you can show and explain.
> The #1 quality bar is **clarity** — a reader should understand any file in one pass.

---

## Architecture (ports & adapters / hexagonal)

The reason the tests are easy is the structure: **dependencies point inward**. `domain/` imports nothing
from infra; adapters implement the ports the domain declares. Domain logic tests need zero infra — pass a fake.

```
   smoke suite (Venom, GraphQL over HTTP, L4)
                                  │
                                  ▼
                          ┌───────────────┐
                          │  gateway (Go) │  gqlgen
                          └──────┬────────┘
                                 │ gRPC  (the ONLY gateway edge)
                                 ▼
        ┌─────────────────────┐  gRPC Estimate (sync quote)  ┌────────────────────┐
        │ voyage-service      │ ───────────────────────────▶ │ estimator-service  │
        │ (Go, gRPC)          │                              │ (Go, gRPC, calc)   │
        │ Postgres + OUTBOX   │ ──voyage.created (Avro)─────▶ │ consumes + prices  │
        │ consumes est.ready  │ ◀──estimate.ready (Avro)───── │ publishes          │
        └─────────────────────┘                              └────────────────────┘

   onboard-sync client (Go library) ──(Toxiproxy: latency/RST/partition)──▶ gateway
        offline-first vessel↔shore sync analog: disk queue, idempotent retry, version-counter LWW

   Observability: /metrics (Prometheus) + OTel traces → Collector → Tempo → Grafana
```

---

## The test pyramid

Rebalanced for microservices: ~60–70% unit, a strong service-integration layer, **contract tests instead
of broad E2E**, a thin E2E top. Push every test to the lowest layer that can catch the bug.

**Every gate ships with a red self-test** — a gate that has never been seen to fail is decoration. Each
layer proves it can go red (breaking-schema fixture, garbage Kafka message, absent metric series, …).

| Layer | What | Tools |
|-------|------|-------|
| **L0** Static | lint + breaking-change gates | golangci-lint, `buf lint/breaking`, gqlgen check, graphql-inspector |
| **L0.5** Mutation | tests must fail when code breaks | go-mutesting over pure-domain pkgs (ratcheted kill-rate floors) |
| **L1** Unit | domain logic, no I/O | testify, hand-written fakes, seeded property tests |
| **L2** Service integration | one service, real DB/Kafka, faked gRPC dep | Testcontainers, Gripmock, Venom |
| **L3** Contract | consumer-driven + schema compat | Pact, Schema Registry FULL-compat, `buf breaking`, graphql-inspector |
| **L4** Whole-stack smoke | 1–2 journeys, zero stubs | Venom HTTP/GraphQL vs compose |
| **L5** Resilience | offline-queue + idempotent retry + LWW under faults | Toxiproxy (timeout + reset_peer) |
| **L6** Perf | error-rate / throughput / lag-drain (latency trend-only) | vegeta, ghz |
| **L7** Shift-right | QA-built monitoring, absent-series alerts | Prometheus rules, `promtool test rules` |

Assertions are **identity-scoped** (`client_request_id`) — never unscoped counts or ordinals.

---

## Project layout

```
services/voyage/        domain/ (pure types + logic + PORT interfaces) · adapters/ (grpcserver, postgres, kafka) · app/ · main.go
services/estimator/     gRPC calc service: consumes voyage.created, prices, publishes estimate.ready
gateway/                gqlgen GraphQL gateway — gRPC is its only edge
clients/onboard-sync/   offline-sync client as a library (L5 drives it in-process)
gen/                    ALL generated code (buf, avrogen, gqlgen) — never hand-edited
schemas/                Avro schemas (history of versioned evolutions)
test/                   venom/ pact/ contract/ schemacheck/ resilience/ perf/ observability/
observability/          Prometheus/Grafana/Tempo/OTel-collector config + dashboards
```

---

## Commands

```
make bootstrap     one-shot dev setup
make ci            THE gate: lint + test-unit + mutate + test-svc + contract + contract-graphql
make test-unit     L1   go test ./... -short -race
make mutate        L0.5 go-mutesting over pure-domain pkgs
make test-svc      L2   testcontainers + gripmock (-p 1)
make contract      L3   pact + seeded SR FULL check
make smoke         L4   venom vs compose stack
make resilience    L5   toxiproxy scenarios
make perf          L6   vegeta + ghz + lag-drain check
```

Run `make ci` before every commit. Smoke / resilience / perf are the on-demand tier.

---

## Scope (deliberate cuts)

A learning lab, scoped down on purpose: **no UI, no CI server, no MCP/AI test management** — gates run
locally via `make ci`. Those are real practices, cut for lab size, not because they're unrealistic. The
*failure modes* (dual-write, redelivery, poison-pill) are real and designed for (outbox, versioned upsert, DLQ).

See [`spec.md`](./spec.md) for the full design and [`CLAUDE.md`](./CLAUDE.md) for the project constitution.
