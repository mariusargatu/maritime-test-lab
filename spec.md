# spec.md — A Production Microservices Testing Stack (sandbox) — v2

> Goal: a runnable **miniature of a real microservices architecture** so you can build the **testing pyramid end-to-end** and *see how the layers connect*. Not a product — a deliberately small system (2 Go gRPC services + Kafka + Postgres + GraphQL gateway + React) that gives every test layer something real to bite.
>
> The spine: **Go unit (slim: testify + hand-written fakes, the calc story) → service integration (Gripmock proto-stubs + Venom + Testcontainers) → contract gates (Pact + schema compat + buf breaking + GraphQL schema checks) → thin whole-stack smoke (Venom vs compose) → resilience (Toxiproxy) → perf → QA-built monitoring shift-right.**
>
> **Scope cuts (2026-06-12, D-036 — deliberate, user-directed):** no webapp/UI, no Playwright, no GitHub Actions CI (gates run locally via `make ci`), no MCP/AI test management. Playwright, GHA and a test-management MCP *are* common real-world practices — cut for lab size, not realism; they're out of lab scope by choice.
>
> **v2** folds in a full design review + stress test (68 verified findings). Biggest changes: hexagonal layout is canonical, the schema/contract gates are rebuilt so they can actually fail, the dual-write/redelivery/poison-pill runtime holes get designed answers (outbox, versioned upsert, DLQ), and every gate ships with a **red self-test**.

---

## 0. Design principles (new in v2)

1. **Every gate must be able to go red.** Each gate ships with a deliberate-failure self-test (a breaking schema fixture, a garbage Kafka message, an absent metric series, a schema-invalid tool call). A gate that has never failed is decoration. This is the meta-lesson of the lab.
2. **Push every test to the lowest layer that can catch the bug.** Contracts replace E2E; L2 replaces cross-service E2E; L1 replaces L2 where logic is pure.
3. **Hexagonal architecture is why the tests are easy.** Domain declares ports; adapters implement them; tests inject fakes. See `CLAUDE.md` (the project constitution — where it and this spec conflict, CLAUDE.md wins).
4. **Faithful to established real-world practice — no invented extras.** Everything here maps to a practice used in production microservices testing. Audited 2026-06-12: mutation testing removed (not common practice here), canary removed in favour of the QA-built-monitor pattern, L5 network simulation kept (D-033–D-035).
5. **Assertions are identity-scoped — never unscoped counts or ordinals.** Every created entity carries a `client_request_id` UUID; SQL/Kafka assertions select by it. An identity-scoped `count(*) WHERE client_request_id = X` equalling 1 *is* the idempotency assertion; what's banned is the unscoped kind (table-wide `count(*)`, `messages0` ordinals) that flakes the moment state is shared. Documented exception: DLQ depth checks — garbage messages have no identity.

---

## 1. Goals / non-goals

**Goals**
- Reproduce each test layer with the *same tool category* used in production.
- Make the **wiring explicit**: what talks to what, what's real vs stubbed at each layer.
- Be buildable **incrementally** (Phase 0→8) — each phase runs green on its own.
- Prove each gate can fail (red self-tests).

**Non-goals**
- Real maritime logic (toy "voyage cost estimator" is the money-calc analog).
- Production hardening. This is a learning lab — but the *failure modes* are real and designed for, because that's the learning.

---

## 2. Reference architecture (the miniature system)

```
   smoke suite (Venom, GraphQL over HTTP, L4)
                                           │
                                           ▼
                                  ┌───────────────┐
                                  │  gateway (Go) │
                                  │  gqlgen       │
                                  └──────┬────────┘
                                         │ gRPC  (the ONLY gateway edge)
                                         ▼
                  ┌─────────────────────┐  gRPC Estimate (sync quote)  ┌────────────────────┐
                  │ voyage-service      │ ───────────────────────────▶ │ estimator-service  │
                  │ (Go, gRPC)          │                              │ (Go, gRPC, calc)   │
                  │ Postgres + OUTBOX   │ ──voyage.created (Avro)────▶ │ consumes + prices  │
                  │ consumes est.ready  │ ◀──estimate.ready (Avro)──── │ publishes          │
                  └─────────────────────┘                              └────────────────────┘

   onBOARD-sync client (Go library, in clients/) ──(Toxiproxy: latency/RST/partition)──▶ gateway
        offline-first analog of vessel↔shore sync: disk queue, idempotent retry, version-counter LWW

   Observability: /metrics (Prometheus) + OTel traces → Collector → Tempo → Grafana
```

**Two estimate paths, on purpose** (this was the #1 ambiguity in v1):
- **Sync:** `CreateVoyage` calls estimator's `Estimate` RPC for a *provisional quote* (this is the downstream dep that gomock fakes at L1 and Gripmock stubs at L2).
- **Async (authoritative):** voyage emits `voyage.created` via a **transactional outbox**; estimator consumes, prices, publishes `estimate.ready`; voyage's consumer applies it with a **versioned idempotent upsert**. This is what the Avro/SR/contract gates bite on.

**Gateway talks ONLY to voyage.** Voyage owns estimate data once `estimate.ready` is applied. One gRPC consumer edge per provider keeps the pact story clean.

**Voyage edits exist from day one.** The proto includes `UpdateVoyage` carrying a per-record **version counter**, and migration #1 adds the `version` column — because L5's conflict scenario needs a server-side merge rule (**voyage-service applies higher-version-wins**), and retrofitting the RPC at Phase 6 would trip buf-breaking, pact, and GraphQL gates all at once. The merge-rule handler itself goes live in Phase 6b.

**The money-calc analog:** `estimator-service` computes cost from distance × rate + fees — the chartering/voyage-estimation correctness stand-in (golden-dataset + seeded property tests).

---

## 3. Component + version matrix (pin these — dev tools too, via go.mod `tool` directives + `make bootstrap`)

| Component | Choice | Real-world analog / why this exact choice |
|---|---|---|
| Language | **Go 1.26** | services + test code |
| RPC | **gRPC** + protobuf, `buf` | internal gRPC. Server **reflection registered** on every service (Venom's grpc executor requires it) |
| Events | **Kafka** via **Redpanda** (single image, **built-in Schema Registry** on 8081 — no separate SR container) | Kafka + Confluent SR (Redpanda's SR is Confluent-API-compatible) |
| Kafka client | **franz-go** + `pkg/sr` Serde (Confluent wire framing: magic byte + schema ID) + `kotel` (trace propagation) | one client where serde, SR framing, and OTel all agree |
| Avro codegen | **hamba/avro v2** `avrogen` → `gen/avro/` | goavro has no codegen and is in maintenance mode |
| DB | **PostgreSQL 16**, migrations via **pressly/goose v3** (embed.FS, applied in `app.Run`) | one DDL source for dev / compose / L2 / L4 |
| Edge | **GraphQL** via `gqlgen` (gateway is **Go**, not TS) | GraphQL gateway (a React front-end is out of lab scope, D-036) |
| Unit | `go test` + **testify**; hand-written fakes for domain ports; **gomock** only in adapter interaction tests. **Slim:** the calc story + one demo per pattern, no coverage gate | the unit layer |
| Service-int | **Venom** + **bavix/gripmock** (pin exact patch, e.g. `3.13.1` — Docker Hub tags have no `v` prefix) + **Testcontainers-go** | service tests |
| Contract (gRPC) | **pact-go v2 + pact-protobuf-plugin** (FFI via `pact-go install`), **brokerless** (monorepo: pact JSON on disk, provider verifies same CI job via `PactFiles`) | contract gates without broker ops |
| Contract (events) | **Pact async message pacts** (semantics) + **seeded SR compat check, mode FULL** (shape) + evolution decode tests | schema gates that can actually fail |
| Contract (GraphQL) | **graphql-inspector** `diff` (breaking-change gate) + `validate` (onboard-sync's committed `.graphql` ops vs schema) | "buf breaking for GraphQL" + consumer-driven check, no SaaS (codegen/tsc gate died with the webapp, D-036) |
| Proto gate | `buf breaking --against '.git#branch=main'` (repo **must be git-initialized with a baseline commit on main** — Phase 0) | proto change safety |
| DB DDL gate | **squawk** (`squawk-cli` via npx) over goose **Up-blocks** of migrations changed vs main (`scripts/lint-migrations.sh`, in `make lint`) | the DB twin of buf breaking / SR FULL — catches DROP/rename/retype/NOT-NULL-without-default; Down-blocks excluded (rollback DROPs are legit); break rules only, lock/style rules off |
| Whole-stack smoke | **Venom** HTTP/GraphQL suite vs the compose stack (Playwright UI E2E is out of lab scope, D-036) | thin top of the pyramid |
| Resilience | **Toxiproxy** (test-owned proxy lifecycle) | vessel-sync network sim |
| Perf | **vegeta** (GraphQL) + **ghz** (gRPC) — Go-native load tools (D-048), noise-immune gates only, latency is trend-only | non-functional |
| Observability | **Prometheus + Grafana + OTel Collector + Tempo** (Grafana stores no traces — Tempo is the trace backend) | shift-right |
| Gates runner | **`make ci` locally** (GH Actions cut, D-036 — repo is local-only; wire later if pushed) | CI, descoped |

---

## 4. The testing pyramid — layer × tool × wiring (the core table)

| # | Layer | Tool | What's REAL vs STUBBED | What it proves | Red self-test |
|---|---|---|---|---|---|
| 0 | Static | golangci-lint, buf lint/breaking, gqlgen regen + `git diff --exit-code` (gqlgen has no `check` command), **graphql-inspector diff**, **squawk** (migration DDL breaking-change) | n/a | no lint/breaking-change regressions on proto, GraphQL **and** Postgres DDL | breaking proto + breaking SDL + **DROP-COLUMN migration** fixtures rejected in CI self-check |
| 1 | **Unit** | testify + hand-written fakes (domain), gomock (adapters) | everything in-proc | pure logic correct (calc golden dataset + seeded property tests); adapter↔client interactions | a deliberately wrong golden row fails |
| 2 | **Service integration** | **Venom** + **Gripmock** + **Testcontainers** | DB/Kafka **REAL** (containers); downstream gRPC **STUBBED** | one service truthful at its boundaries: rows, events, **idempotent replay**, **poison-pill survival**, **outbox delivery after broker outage** | the poison-pill and duplicate-replay cases ARE red self-tests |
| 3 | **Contract** | pact-go v2 (+protobuf plugin, + message pacts) + **seeded** SR FULL-compat check + buf breaking | consumer & provider tested separately against shared contracts | producer change can't silently break a consumer — sync, async, and GraphQL edges all gated | known-breaking .avsc fixture must be rejected; CI asserts it |
| 4 | **Whole-stack smoke (thin)** | Venom (HTTP/GraphQL) vs compose | whole stack REAL (healthchecked), zero stubs | 1–2 journeys; `create voyage → estimate appears` crosses the full async loop via GraphQL polling steps | — (thin by design) |
| 5 | **Resilience** | Toxiproxy (timeout **and** reset_peer toxics) | real stack, degraded network | disk-queued offline writes, idempotent retry (no dupes), version-counter conflict resolution, **survives client SIGKILL+restart** | assertions read client `SyncStatus()` + server rows + **/metrics counters** — never sleeps |
| 6 | **Perf** | vegeta + ghz | real stack under load | error rate, throughput floor, **consumer-lag drains after load**; latency trend-only | lag-gate self-check: query must return series or fail |
| 7 | **Shift-right** | Prometheus + OTel/Tempo + **QA-built monitor** (their invoicing-monitor pattern) | production-like | estimate-pipeline monitor: alert rules (latency p99, error rate, DLQ depth, lag) **unit-tested with `promtool test rules`** + alert route | absent metric series must ALERT (no-data ≠ healthy); promtool red-rule fixtures |

*(L8 AI/MCP removed by scope cut D-036.)*

---

## 5. Repo layout

```
maritime-test-lab/                # git repo (init -b main in Phase 0 — buf breaking needs the baseline)
├─ proto/                          # .proto contracts (buf-managed)
│  ├─ voyage/v1/voyage.proto       #   CreateVoyage (client_request_id UUID) + UpdateVoyage (version counter) from day one
│  └─ estimator/v1/estimator.proto #   sync Estimate RPC (the Gripmock-stubbed dep)
├─ schemas/                        # Avro schemas (Kafka events)
│  ├─ voyage_created.avsc
│  ├─ estimate_ready.avsc          #   carries voyage_version (monotonic) for idempotent upsert
│  └─ history/                     #   prior versions kept for evolution decode tests (v1.avsc, v2.avsc…)
├─ gen/                            # ALL generated code (buf.gen.yaml, avrogen, gqlgen) — never hand-edited
├─ services/
│  ├─ voyage/                      # hexagonal — see CLAUDE.md
│  │  ├─ domain/                   #   pure types + logic + PORT interfaces; *_test.go = L1 (fakes)
│  │  ├─ adapters/
│  │  │  ├─ grpcserver/            #   inbound (implements proto service; registers reflection)
│  │  │  ├─ postgres/              #   outbound (VoyageRepository) + migrations/ (goose, embed.FS) + outbox table
│  │  │  └─ kafka/                 #   outbound publisher (outbox poller) + inbound estimate.ready consumer (franz-go)
│  │  ├─ app/                      #   wiring: migrate → construct adapters → inject → run; graceful shutdown
│  │  └─ main.go                   #   thin: config → app.Run
│  └─ estimator/                   # same hexagonal shape but STATELESS: no DB, no outbox —
│                                  #   consume → price → publish; at-least-once via offset discipline. calc = golden-dataset target
├─ clients/
│  └─ onboard-sync/                # Go LIBRARY (driven in-process by L5 tests): disk queue (JSONL/bbolt,
│                                  #   keyed by client_request_id), ctx deadlines, version-counter LWW,
│                                  #   SyncStatus() accessor, injected clock. Compose container = demo only.
│                                  #   Its GraphQL ops live in committed .graphql files — part of the
│                                  #   consumer contract corpus (validated by contract-graphql, same as webapp).
├─ gateway/                        # gqlgen (Go). schema.graphqls is the committed GraphQL contract artifact
├─ test/
│  ├─ venom/                       # L2 suites (see conventions in §8-L2)
│  │  └─ smoke/                    # L4 whole-stack smoke suite (GraphQL over HTTP vs compose)
│  ├─ gripmock/                    # bavix v3 stub files (happy-path defaults only; suites purge+post their own)
│  ├─ pact/                        # L3: consumer tests + pacts/ output dir + provider verification
│  ├─ resilience/                  # L5 scenarios (`go test -tags=resilience`)
│  └─ perf/                        # L6 vegeta + ghz + lag-drain checker
├─ observability/                  # prometheus.yml + RULES (promtool-tested) + otel-collector + tempo + dashboards
├─ docker-compose.yml
├─ .env                            # committed: parameterized host ports (off well-known defaults)
├─ package.json                    # devDeps: @graphql-inspector/cli + squawk-cli (tooling-only npm gates)
└─ Makefile
```

---

## 6. docker-compose (the shared substrate — L4/L5/L6 + local dev; L2 uses Testcontainers instead)

```yaml
# docker-compose.yml (sketch — the load-bearing details are listeners, healthchecks, ports)
services:
  postgres:
    image: postgres:16.6
    environment: { POSTGRES_PASSWORD: dev }
    ports: ["${PG_PORT:-15432}:5432"]            # off 5432 — Homebrew PG collision is real
    healthcheck: { test: ["CMD-SHELL", "pg_isready -U postgres"], interval: 2s, retries: 15 }

  redpanda:
    image: redpandadata/redpanda:v25.1.1          # ≥ v25.1: enable_consumer_group_metrics (lag gauges) doesn't exist in 24.x
    command: >
      redpanda start --mode dev-container --smp 1 --memory 256M
      --kafka-addr internal://0.0.0.0:9092,external://0.0.0.0:19092
      --advertise-kafka-addr internal://redpanda:9092,external://localhost:19092
      --schema-registry-addr 0.0.0.0:8081
    ports: ["19092:19092", "${SR_PORT:-18081}:8081"]   # in-network: redpanda:9092 / host: localhost:19092
    healthcheck: { test: ["CMD-SHELL", "rpk cluster health | grep -q 'Healthy:.*true'"], interval: 2s, retries: 20 }

  topic-init:                                     # explicit topics — dev-container mode ≠ prod assumptions
    image: redpandadata/redpanda:v25.1.1
    depends_on: { redpanda: { condition: service_healthy } }
    entrypoint: ["sh", "-c",                       # idempotent: re-up against live broker must not fail
      "rpk -X brokers=redpanda:9092 topic create voyage.created estimate.ready voyage.created.dlq estimate.ready.dlq || true"]

  voyage:
    build: ./services/voyage
    depends_on: { postgres: { condition: service_healthy }, topic-init: { condition: service_completed_successfully } }
    healthcheck: { test: ["CMD", "/bin/grpc_health_probe", "-addr=:9000"], interval: 2s, retries: 15 }
    restart: on-failure

  estimator:
    build: ./services/estimator
    depends_on: { topic-init: { condition: service_completed_successfully } }
    healthcheck: { test: ["CMD", "/bin/grpc_health_probe", "-addr=:9000"], interval: 2s, retries: 15 }
    restart: on-failure

  gateway:
    build: ./gateway
    profiles: ["edge"]                            # only when a layer needs the front half (L4/L5/L6)
    depends_on: { voyage: { condition: service_healthy } }   # gateway's ONLY edge
    ports: ["${GATEWAY_PORT:-18080}:8080"]        # published (off 8080 on the host) — Playwright + toxiproxy upstream need it
    healthcheck: { test: ["CMD-SHELL", "wget -qO- http://localhost:8080/healthz || exit 1"], interval: 2s, retries: 15 }
    restart: on-failure

  toxiproxy:
    image: ghcr.io/shopify/toxiproxy:2.12.0
    profiles: ["chaos"]                           # L5 only
    ports: ["8474:8474", "8666:8666"]             # proxies created AT RUNTIME by L5 TestMain (listen 0.0.0.0!)

  # ── observability: profile "obs" — DEMO + canary only. No test gate needs these containers:
  #    L5 scrapes /metrics directly; L6 lag-drain uses franz-go admin API; promtool tests rules with no server.
  prometheus:  { image: prom/prometheus:v3.2.0, profiles: ["obs"], volumes: ["./observability/prometheus.yml:/etc/prometheus/prometheus.yml", "./observability/rules:/etc/prometheus/rules"], ports: ["${PROM_PORT:-19090}:9090"] }
  tempo:       { image: grafana/tempo:2.7.1, profiles: ["obs"] }   # the trace backend Grafana reads from
  otel-collector: { image: otel/opentelemetry-collector-contrib:0.121.0, profiles: ["obs"], volumes: ["./observability/otel-collector.yaml:/etc/otelcol-contrib/config.yaml"] }
  grafana:     { image: grafana/grafana:11.5.2, profiles: ["obs"], ports: ["${GRAFANA_PORT:-13000}:3000"] }  # 3000 belongs to nobody
```

> Notes: no `cp-schema-registry` (Redpanda's built-in SR serves 8081); no gripmock service (L2 runs it via Testcontainers; L4+ runs the real estimator); CI brings the stack up with `docker compose up -d --wait`. Enable Redpanda lag metrics once in Phase 7: `rpk cluster config set enable_consumer_group_metrics '["group","partition","consumer_lag"]'` (requires the v25.1+ pin above).
>
> **Compose grows with the phases.** Phase 0 ships postgres + redpanda + topic-init + the voyage/estimator skeletons (their build contexts exist from Phase 0). gateway joins in Phase 5, toxiproxy in Phase 6b, the observability quartet in Phase 7 — a service never appears in compose before its build context exists, or `compose up` fails on missing directories.
>
> **Resource tiers (laptop-friendly by construction).** Profiles mean you only ever pay for the layer you're working:
>
> | Mode | Command | Containers | ~RAM |
> |---|---|---|---|
> | daily dev | `compose up -d` | pg + redpanda(256M) + 2 Go services | < 700 MB |
> | smoke (L4) | `--profile edge` | + gateway | < 800 MB |
> | resilience (L5) | `--profile edge --profile chaos` | + toxiproxy | < 850 MB |
> | perf/monitor (L6/L7) | `--profile edge --profile obs` | + prom/tempo/otel/grafana | ~1.5 GB peak, only while `make perf` runs |
> | L2 tests | none (testcontainers) | one ephemeral pg+redpanda set, `-p 1` serialized | < 700 MB |
>
> A 6 GB Docker VM covers the worst case. Nothing in the **test gates** needs the obs profile: L5 asserts on `/metrics` directly, L6's lag-drain checker computes lag via franz-go's admin API against the broker, `promtool` tests rules with no server. The obs containers exist for the Grafana demo and the canary-check window only.

---

## 7. Makefile targets (your daily verbs)

```makefile
bootstrap:        ## one-shot dev setup: go tools via `go tool` directives (buf, mockgen, gqlgen, avrogen, ghz, vegeta, venom,
                  ## golangci-lint, goose — pinned in go.mod); pact-go install (FFI)
                  ## + pact-plugin-cli install protobuf (the gRPC plugin is a SEPARATE install from the FFI);
                  ## npm ci at root (pins @graphql-inspector/cli + squawk-cli — the lab's npm tooling deps)
doctor:           ## prints docker context/socket, runs a hello-world testcontainer (Colima/Podman sanity)
generate:         ## buf generate + gqlgen + avrogen → gen/ ; mockgen → adapter TEST packages (never gen/)
lint:             ## golangci-lint + buf lint + buf breaking --against '.git#branch=main'
                  ## + graphql-inspector diff 'git:main:gateway/schema.graphqls' gateway/schema.graphqls
                  ##   (LOCAL branch ref, same as buf — Phase 0 has no origin remote)
migrate:          ## goose up via cmd/migrate (same embed.FS path app.Run uses)
test-unit:        ## L1  go test ./... -short -race  (slim layer: calc story + pattern demos; no coverage gate, D-036)
test-svc:         ## L2  go test -tags=integration -p 1 ./...   (-p 1: one PG+Redpanda set at a time; TestMain shares per package)
venom:            ## L2  TestMain writes test/venom/env.yml with the live fixture addresses (dynamic testcontainer
                  ##     ports), then execs `venom run test/venom/ --var-from-file=env.yml` — that's the wiring
contract:         ## L3  pact consumer tests → test/pact/pacts/ → provider verify (PactFiles, same job)
                  ##     + message pact verify + seeded SR FULL-compat check + red self-test fixture
contract-graphql: ## L3  graphql-inspector validate 'clients/onboard-sync/**/*.graphql' gateway/schema.graphqls --deprecated
                  ##     (onboard-sync is the lab's GraphQL consumer; webapp/codegen gate cut with the UI, D-036)
smoke:            ## L4  docker compose --profile edge down -v && up -d --wait && venom run test/venom/smoke/
resilience:       ## L5  compose --profile edge --profile chaos up -d --wait && go test -tags=resilience ./test/resilience/...
perf:             ## L6  compose --profile edge --profile obs up -d --wait; vegeta + ghz; lag-drain via franz-go admin API (no Prometheus in the gate)
ci:               ## THE gate (local — no GitHub Actions, D-036): lint test-unit test-svc contract contract-graphql
```

> **Targets are phase-aware:** each sub-step activates in the phase that creates its inputs (graphql-inspector steps from Phase 5, `contract` from Phase 4, `perf` from Phase 7…). Until then the Makefile guards on file existence and skips with a printed notice — `make ci` is green from Phase 0 onward. Run `make ci` before every commit; `make smoke`/`resilience`/`perf` on demand (the "nightly" tier, minus the CI server).

---

## 8. Per-layer specifics (the connection details + the traps v1 missed)

### L1 — Unit (testify; fakes in domain, gomock in adapters)
- **Domain tests:** hand-written fakes implementing the domain's own ports (e.g. `Estimator`, `VoyageRepository`). No gRPC/sql/kafka types anywhere near `domain/`.
- **Adapter tests:** gomock against the generated gRPC client — verify the call is made once, request fields map correctly, infra errors translate to domain errors. This is where `mockgen` output lives (adapter test packages), not a top-level `mocks/`.
- **Money-calc:** golden-dataset table tests + **seeded** property tests (monotonic in distance, never negative) — the explicit, labelled exception to the "no randomness in tests" rule.
- **Parity test (cross-representation drift):** one table test asserts field-name parity between the voyage proto message and the `voyage_created` avro struct, minus an explicit allowlist. Catches "added `currency` to proto+SQL+GraphQL, forgot the event".
- **Evolution decode test:** for each `schemas/history/v*.avsc`, encode with the old writer schema, decode with the current reader (and current-writer → previous-reader). SR compares schemas algebraically; this tests the actual bytes path.

### L2 — Service integration (Venom + Gripmock + Testcontainers) — the heart
1. **TestMain per package** boots ONE shared Postgres + Redpanda (built-in SR) fixture; `make test-svc` runs `-p 1` so packages serialize (one container set alive at a time; budget 3–6 min, documented). Testcontainers' Redpanda module auto-wires advertised listeners and exposes the SR URL.
2. **bavix/gripmock:3.13.1** (Testcontainer; tags have no `v` prefix — `:v3` does not exist) loads `estimator.proto`; **every Venom suite's first step purges stubs** (`DELETE /api/stubs`) **then POSTs exactly the stubs it needs**. Static stub files = happy-path defaults only. (Stub bleed between suites = the classic "passes alone, fails together".)
3. Start `voyage-service` with env pointed at the fixtures. Service registers gRPC **reflection** (Venom needs it).
4. **Venom conventions (every kafka consume step):** `initialOffset: oldest`, unique `groupID` per testcase, `messageLimit`, step-level `retry: 10, delay: 1`. **All SQL/Kafka assertions filter on `client_request_id`** — never counts, never `messages0` ordinals.

```yaml
# test/venom/voyage_create.venom.yml (Phase 2 scope: gRPC + SQL; kafka step added in Phase 3)
name: voyage create is idempotent and persists
testcases:
  - name: create voyage twice with same client_request_id   # ← idempotency contract, PR-gated from day one
    steps:
      - type: grpc
        url: "{{.voyage_addr}}"
        service: voyage.v1.VoyageService
        method: CreateVoyage
        data: { client_request_id: "{{.uuid}}", origin: "NLRTM", dest: "SGSIN", distance_nm: 8200 }
        assertions: [ "result.code ShouldEqual 0" ]
      - type: grpc          # same request again — must succeed, not AlreadyExists
        url: "{{.voyage_addr}}"
        service: voyage.v1.VoyageService
        method: CreateVoyage
        data: { client_request_id: "{{.uuid}}", origin: "NLRTM", dest: "SGSIN", distance_nm: 8200 }
        assertions: [ "result.code ShouldEqual 0" ]
      - type: sql                  # NB: the sql executor takes `commands:` (a list), NOT `query:`,
        driver: postgres           #     and results are addressed result.queries.queriesN.rows.rowsN.<col>
        dsn: "{{.pg_dsn}}"
        commands:
          - "select count(*) as n from voyages where client_request_id = '{{.uuid}}'"
        assertions:
          - result.queries.queries0.rows.rows0.n ShouldEqual 1
```

**Phase 3 adds to this suite:** the kafka consume step (`with_avro: true`, `schema_registry_addr`), the **duplicate-redelivery case** (publish same `voyage.created` twice + an out-of-order version pair → exactly one correct estimate), the **poison-pill case** (one garbage message, then one valid → valid still estimated, DLQ holds exactly one), and the **outbox case** (broker down → CreateVoyage still succeeds → broker up → event appears).

### L3 — Contract + schema gates (rebuilt so they can fail)
- **gRPC pacts (pact-go v2 + pact-protobuf-plugin; FFI via `pact-go install` in bootstrap + CI):** **brokerless** — consumer tests write pact JSON to `test/pact/pacts/`; provider verification runs in the same CI job via `VerifyRequest.PactFiles`. Pairs: **voyage→estimator** (Phase 4 — both sides exist), **gateway→voyage** (Phase 5, written against the gateway's real client code). Provider states include `"a voyage with client_request_id X already exists"` so idempotency semantics are part of the replayed contract.
- **Message pacts (the async edges):** estimator-as-consumer for `voyage.created`, voyage-as-consumer for `estimate.ready` (pact-go `AddAsynchronousMessage`, verified against the real event-builder functions). SR checks shape; message pacts catch **content/semantics drift** (km-vs-nm, default-0 fields) that no schema gate can see.
- **SR compat — seeded, FULL, check-only:** CI seeds the ephemeral SR from main's schemas (`git show main:$f` per file — the `rev:path` form takes no wildcards, so loop via `git ls-tree -r --name-only main -- schemas/`; local branch ref, no remote needed), sets `compatibility=FULL` (BACKWARD is wrong for a producer-first topology: it *allows* the breaking direction), then runs the **check-only** endpoint on the PR's schemas. An unseeded subject 404s loudly instead of passing silently. **Red self-test:** a known-incompatible fixture must be rejected or the job fails.
- **buf breaking** vs `.git#branch=main` (repo is git-initialized with the baseline in Phase 0).
- **GraphQL (the edge v1 left ungated):** `graphql-inspector diff` in `make lint` (breaking-change gate vs main's SDL) + `graphql-inspector validate` over the lab's GraphQL consumer — `clients/onboard-sync/**/*.graphql` (the sync client's disk queue stores serialized mutations; a renamed mutation field would brick every queued write on reconnect). No broker, no SaaS. (The webapp corpus + codegen/tsc second net died with the UI, D-036.)
- **Runtime serde must match the gate:** services use franz-go `pkg/sr` Serde → Confluent wire framing (magic byte + schema ID), registering schemas at startup under **TopicNameStrategy subjects (`<topic>-value`)** — the convention Venom's `with_avro` decode and the seeded compat check both assume. Otherwise Venom mis-parses and the SR is decorative at runtime.
- **Commit discipline (the silent-loss direction):** consumers disable autocommit and commit offsets **only after the side effect completes** — estimator: after the `estimate.ready` produce is acked; voyage: after the versioned upsert commits. Graceful-shutdown order in `app.Run`: stop fetching → flush producer → `CommitUncommittedOffsets` → close. (The versioned upsert handles the duplicate direction; this handles the loss direction — autocommit-before-processing silently drops estimates on crash.)

### L4 — Whole-stack smoke (Venom vs compose, thin)
- `make smoke` = `compose --profile edge down -v` (clean DB) → `up -d --wait` (healthchecks gate readiness) → `venom run test/venom/smoke/`.
- **1–2 journeys only.** The flagship: GraphQL `createVoyage` mutation (HTTP step) → assert provisional quote in the response → **poll the `voyage(id)` query with Venom step `retry: 15, delay: 2`** until the authoritative estimate appears. The ~30s budget = Kafka round trip; documented in the suite header so nobody shrinks it. Second case: duplicate `createVoyage` (same `client_request_id`) → same voyage back, not an error.
- What distinguishes this from L2: **zero stubs, real gateway, real estimator, compose substrate** — it proves the wiring, not the logic. Unique data per journey (UUID), identity-scoped assertions, as everywhere.
- *(Playwright UI E2E is consciously out of lab scope, D-036 — no UI exists here.)*

### L5 — Resilience / vessel-sync (Toxiproxy) — the differentiator
- **The client is a library** (`clients/onboard-sync`), driven **in-process** by `go test -tags=resilience`: disk-persisted queue (append-only JSONL or bbolt, keyed by `client_request_id`, entries removed on server ack), explicit `context.WithTimeout` on every request (deadlines ARE the offline-detection design), injected clock, `SyncStatus()` (queue depth, retry count, last attempt). The compose container form is demo-only — **never assert through it**.
- **Conflict resolution: monotonic per-record version counter** (higher sync sequence wins). No clocks in the merge decision at all — deterministic, trivially assertable; clock-skew LWW is the trap, not the lesson.
- **Toxiproxy lifecycle is test-owned:** L5 TestMain creates the proxy via the Go client (`CreateProxy("gateway", "0.0.0.0:8666", "gateway:8080")` — `0.0.0.0`, or it's unreachable from the host), `defer ResetState()` per scenario (toxics leak across failed tests otherwise).
- **Two failure shapes per scenario, table-driven:** `timeout` toxic (black hole — open socket, data dropped; only request deadlines detect it) and `reset_peer`/proxy-disable (RST — the instant-error path; different client code path).
- **Scenarios:** (1) partition → enqueue N → heal → all N applied, `select count(*) where client_request_id=...` = 1 each; (2) partition → enqueue → **simulated crash → restart** → heal → nothing lost, no dupes (this is what makes "offline-first" true). Canonical crash mechanism: the test **abandons the live client instance and cold-reopens the disk queue from a fresh instance** — in-process, deterministic, no signals; an `os/exec`-spawned helper binary that gets SIGKILLed is the optional realism variant, not the gate; (3) conflicting concurrent edits via `UpdateVoyage` → **voyage-service applies the merge rule (higher version counter wins)** → winner asserted via GraphQL read.
- **Assertions** use `require.Eventually` on `SyncStatus()`, server rows by `client_request_id`, and **scraped `/metrics` counters** (retry/dedupe counters via `expfmt`) — observability becomes a tested surface here, not a Phase 7 hope.

### L6 — Perf (gates that survive noisy infra)
- vegeta on GraphQL, ghz on estimator gRPC (both Go-native, D-048).
- **Gate only on noise-immune signals:** error rate < 1%, throughput floor, **consumer-lag drains to ~0 within X s after load stops**, plus one loose pathology guard (p95 < 1s). The lag-drain checker computes lag with **franz-go's admin API (`kadm.Lag`) directly against the broker** — no Prometheus in the gate, so `make perf`'s pass/fail needs zero obs containers. (The `consumer_lag` Prometheus metric stays enabled for the Grafana dashboard — demo surface, not gate.)
- **Latency is trend-only:** export the vegeta report (JSON) as a CI artifact; regression = 3 consecutive nights above baseline, not one bad run on a shared runner. Hard p95<300ms on Docker Desktop = a gate you'll delete by week 3.
- No backpressure **by design** (Kafka absorbs bursts); the gate is drain time + end-to-end estimate latency, not lag-during-burst. Written here so nobody "fixes" lag by making CreateVoyage block.

### L7 — Shift-right (the smallest honest canary)
- Services expose `/metrics` + OTel traces → Collector → **Tempo** → Grafana. Kafka trace continuity via franz-go **kotel** hooks (W3C tracecontext in record headers) — without it the flagship trace demo is two disconnected stubs.
- **SLO as code:** Prometheus recording + alert rules in `observability/rules/`, **unit-tested with `promtool test rules`** — the shift-right layer's executable test.
- **QA-built monitor (a common real-world pattern — the invoicing-monitor analog):** an **estimate-pipeline monitor** built the way QA teams build production monitors. Alert rules: estimate end-to-end latency p99, error rate, **DLQ depth > 0**, consumer-lag growth, and an **absent-series alert** — a renamed/vanished metric must ALERT, never read as healthy. Alert route: webhook to a tiny local log sink (the Slack-automation stand-in). Every rule has promtool unit tests including red fixtures (a synthetic series that MUST fire each alert). Metric names live as consts in one shared package used by both instrumentation and the rules tests — rename a metric, the rules test fails at PR time.
- No canary, no deploy gate — the chosen shift-right pattern is *QA builds monitoring*, not canary deployments (realism audit D-035). The vegeta run (L6) doubles as the demo traffic that makes the monitor light up in Grafana.

### L8 — removed (scope cut D-036)
The Zephyr-style MCP + AI-governance layer is out of lab scope by user decision. It IS a real-world practice (test-management MCP, prompt-validation governance) — out of lab scope by choice.

---

## 9. The gate tiers (local — GitHub Actions cut, D-036)

```
every commit:   make ci      = lint (golangci + buf breaking + inspector diff + gqlgen-regen check)
                              + test-unit (-race) + test-svc (-p 1) + contract + contract-graphql
on demand:      make smoke   = whole-stack journey vs compose          (run before calling a phase done)
"nightly" tier: make resilience / make perf                            (manual or cron — no CI server)
```

- Discipline replaces automation: `make ci` before every commit is the contract with yourself; the Makefile IS the pipeline definition. If the repo is ever pushed, each tier maps 1:1 onto a GH Actions job (the spec's earlier CI design lives in git history — recoverable, not deleted knowledge).

---

## 10. Build order (phases — each ends green)

| Phase | Deliverable | Layers live |
|---|---|---|
| 0 | `git init -b main` + baseline commit; proto (incl. `client_request_id`, **`UpdateVoyage` + version counter**, sync `Estimate` RPC) + buf; `schemas/voyage_created.avsc` + avrogen wiring (Phase 1's parity test needs it); 2 hexagonal service skeletons (reflection on); goose migration #1 (voyages + **unique index on client_request_id** + **version column**); compose substrate — infra services only: postgres/redpanda/topic-init + the two skeletons (healthchecks, listener split, parameterized ports, pinned tags); `make bootstrap` + `doctor`; `buf.gen.yaml` → `gen/` | 0 |
| 1 | calc logic + L1: golden dataset, seeded property tests, domain fakes; adapter interaction tests (gomock); parity-test scaffold | 1 |
| 2 | Testcontainers TestMain fixtures (+ `-p 1`); bavix Gripmock + purge protocol; first Venom suite (**gRPC + SQL only**, incl. the duplicate-`client_request_id` idempotency case) | 2 |
| 3 | **Both events end-to-end:** transactional outbox + poller; franz-go + sr Serde (Confluent framing); estimator consume→price→publish; voyage's `estimate.ready` consumer (versioned idempotent upsert); DLQ policy; graceful shutdown in `app.Run`; `estimate_ready.avsc`; **seeded SR FULL gate + red fixture**; evolution decode + parity tests live; Venom kafka steps (+ redelivery, poison-pill, outbox cases) | 2–3 |
| 4 | Pact brokerless: **voyage→estimator** gRPC pair + **both message pacts**; full `make contract` green | 3 |
| 5 | gateway (gqlgen, Go) joins compose; **gateway→voyage pact** + GraphQL gates (inspector diff/validate over onboard-sync ops) + **L4 smoke suite** (Venom HTTP/GraphQL: flagship async journey + duplicate-submit, down -v → up --wait, retry-poll, 30s budget) | 4 |
| 6a | `clients/onboard-sync` **library**: disk queue, deadlines, version-counter LWW, `SyncStatus()`, injected clock — with its own L1 tests | 1 |
| 6b | Toxiproxy joins compose; L5 scenarios: test-owned proxy lifecycle, timeout + reset_peer shapes, crash+cold-reopen, /metrics assertions; **voyage's `UpdateVoyage` merge rule goes live (higher version wins)** | 5 |
| 7 | Observability quartet joins compose (`--profile obs`): Prometheus (+ `consumer_lag` metrics) + OTel + Tempo + Grafana + kotel propagation; vegeta/ghz with noise-immune gates + lag-drain check; **estimate-pipeline monitor** (promtool-tested alert rules + absent-series alert + webhook sink). **Final phase — lab complete.** | 6–7 |

---

## 11. Real-world tool → why → sandbox mapping (cheat)

| Production uses | Because | You reproduce with |
|---|---|---|
| Venom | YAML integration tests, multi-protocol | Venom (same; reflection on, oldest-offset conventions; also drives the L4 smoke) |
| Gripmock | stub gRPC deps in service tests | bavix/gripmock v3 (maintained successor) |
| Playwright+TS | UI E2E | **out of lab scope (D-036 — no UI built)** |
| Ginkgo/Gomega/envtest | operator/platform tests | optional: tiny controller + envtest |
| Pact / contract | microservice change safety | pact-go v2 + protobuf plugin + message pacts (brokerless) |
| Kafka + Schema Registry | event-driven + schema evolution | Redpanda + its built-in Confluent-compatible SR |
| network-conditions simulator | vessel↔shore degraded links | Toxiproxy (timeout + RST shapes) |
| Test-management MCP + prompt-validation governance | AI-assisted test mgmt, governed | **out of lab scope (D-036)** |
| Prometheus/Grafana/Sentry/Sensu — "QA builds monitoring" | shift-right observability | Prometheus + OTel + Tempo + Grafana + the estimate-pipeline monitor (invoicing-monitor analog) |

---

## 12. Decision log (the "why" — the rationale)

| Decision | Why (one breath) |
|---|---|
| Outbox over dual-write | crash between DB commit and publish is invisible to every test layer; outbox makes it a polled table you can assert on |
| Versioned idempotent upsert | at-least-once delivery is a *promise* of redelivery; consumers must be safe to replay by construction |
| DLQ + skip on decode error | one poison message must cost one DLQ entry, not the whole consumer |
| FULL compat, seeded check | BACKWARD permits the exact break a producer-first deploy ships; an unseeded registry approves anything |
| Brokerless Pact | a monorepo with one CI doesn't need broker ops; `PactFiles` gives the same guarantee with zero infra |
| GraphQL inspector gates | the webapp edge was the only ungated interface; `validate` makes it consumer-driven without SaaS |
| DB DDL breaking gate (squawk) | Postgres DDL was the last contract surface with no breaking-change gate (proto/Avro/GraphQL each had one); squawk on migration Up-blocks makes the 4th surface symmetric — break rules only, not zero-downtime advice (D-049) |
| Version counter over LWW timestamps | "last" needs a definition; a monotonic counter is deterministic and clock-free |
| Library-first onBOARD client | a test that can only watch a container must sleep; a library exposes `SyncStatus()` and an injected clock |
| Latency trend-only | a hard p95 gate on shared runners red-flags noise until someone deletes it; drain-time and error-rate gates survive |
| Red self-tests everywhere | a gate that has never been seen to fail is indistinguishable from a gate that can't |
| Mutation testing removed; canary → QA-built monitor | realism audit: neither is common production practice here; the shift-right pattern is "QA builds monitoring", the AI governance is prompt validation + human approval |
| L5 kept after the same audit | the network-conditions simulator is a real production practice — the resilience deep-dive topic |
| Scope cuts: no UI/Playwright, no GH Actions, no MCP (D-036) | user-directed simplification — these ARE real-world practices, cut for lab size, not realism; the lab's pyramid is L0–L7 with a Venom smoke top |
| L1 kept slim | testify+gomock at base is documented practice and the calc-correctness story lives there; coverage gate dropped, patterns kept |

---

## 13. Implementation defaults (decided pre-Phase-0)

| Choice | Decision |
|---|---|
| Go module | **single module**, path `maritime-test-lab` (local placeholder — repo is local-only for now; CI workflow ships but runs only when pushed) |
| Money | int64 minor units + currency code, **USD-only v1**; Avro side = `long` cents. Never float. |
| Postgres driver | pgx/v5 (+ stdlib adapter for goose) |
| gRPC ports | voyage :9000, estimator :9001; standard grpc health service + `grpc_health_probe` in images |
| Outbox | flag-on-ack + periodic cleanup (keeps the audit trail visible), poll 250ms |
| Disk queue | JSONL append-only (readable in demos beats bbolt) |
| Node | 22 LTS via `.nvmrc`; npm — tooling only: `@graphql-inspector/cli` + `squawk-cli` (root package.json) |
| Auth | **none, by design** — out of scope for the lab; journeys share one namespace, hence unique-data-per-journey rule |
| Resource model | compose **profiles** (`edge`/`chaos`/`obs`) — daily dev < 700 MB, worst case ~1.5 GB only during `make perf`; no test gate depends on the obs containers; opt-in `TESTCONTAINERS_REUSE` for the L2 dev loop |

---

### One-line mental model
> "Correctness at L1/L2, **change-safety via contracts at L3 on every edge — gRPC, events, and GraphQL**, a thin whole-stack smoke at L4, **resilience for the vessel-sync reality at L5**, QA-built monitoring as the shift-right net — **and every gate ships with a proof it can fail**."
