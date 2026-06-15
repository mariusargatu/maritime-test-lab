# CLAUDE.md — maritime-test-lab

A small, **deliberately readable** reference system that reproduces a production microservices testing stack (see `./spec.md`) — **faithfully: only practices used in real-world testing, no invented extras** — deliberately scoped down (no UI, no CI server, no MCP; spec D-036). Two Go gRPC services + Kafka/Avro + Postgres + a GraphQL gateway, wrapped in the test pyramid (static → unit → service-integration → contract → whole-stack smoke → resilience → perf → shift-right monitoring).

**Why it exists:** to *learn how the test layers connect* and to be a worked example you can show and explain. Therefore the #1 quality bar is **clarity** — a reader should understand any file in one pass.

---

## Prime directives (in priority order)

1. **Readable beats clever.** Optimise for the next human reading it, not for lines saved. If a senior engineer can't grasp a function in ~20 seconds, rewrite it.
2. **Testable by construction.** Architecture (ports & adapters) makes tests easy *without* mocking frameworks fighting you. If something is hard to test, the design is wrong — fix the design, not the test.
3. **Maintainable over time.** Small files, explicit dependencies, no hidden global state, errors that explain themselves. Boring, consistent code.

> When directives conflict, **earlier wins**. Readability > a clever testable trick > a maintainability rule of thumb.

---

## Architecture: ports & adapters (hexagonal)

The reason the tests are easy is this structure. Keep it.

```
            adapters (I/O, replaceable)            domain (pure, the point)
  gRPC handler ─┐                          ┌─ Voyage, Estimate (types)
  Kafka consumer├─▶  PORTS (interfaces) ──▶ │  CostCalculator      (logic)
  Postgres repo─┘     defined HERE,         └─ no imports of grpc/sql/kafka
                      by the domain
```

**The dependency rule:** dependencies point **inward**. `domain/` imports nothing from `adapters/`. Adapters import the domain. Infra (DB, Kafka, gRPC) lives only in adapters.

**Ports live with the consumer.** The domain declares the *interfaces it needs* (e.g. `VoyageRepository`, `EventPublisher`); adapters implement them. This is Go-idiomatic ("accept interfaces, return structs") and means **domain logic tests need zero infra** — pass a fake.

**Keep interfaces small.** 1–3 methods. A big interface is a smell; split it.

---

## Project layout

```
services/voyage/
  domain/            # pure types + logic + PORT interfaces. NO grpc/sql/kafka imports.
    voyage.go
    voyage_test.go   # L1 unit — fast, no I/O
  adapters/
    grpcserver/      # inbound adapter (implements proto service; registers gRPC reflection — Venom needs it)
    postgres/        # outbound adapter (implements domain.VoyageRepository)
      migrations/    # goose SQL files, embed.FS — the ONE DDL source (dev, compose, L2, L4 all apply it via app.Run)
    kafka/           # outbound publisher (transactional-outbox poller) AND inbound consumer (estimate.ready → versioned idempotent upsert; offsets committed only AFTER the upsert — autocommit off)
  app/               # wiring: migrate → construct adapters → inject → run; graceful shutdown (signal.NotifyContext)
  main.go            # thin: read config, call app.Run
clients/onboard-sync/ # offline-sync client as a LIBRARY (L5 tests drive it in-process; SyncStatus() + injected clock)
gen/                 # ALL generated code (buf, avrogen, gqlgen). Never hand-edited. gomock output lives in adapter test pkgs.
test/                # cross-service: venom/ (incl. smoke/) pact/ contract/ schemacheck/ resilience/ perf/ observability/ (gripmock runs as a testcontainer via internal/testfix, not a test/ subdir)
```

- **One responsibility per file.** Prefer **many small files** (150–300 lines, hard cap 500) over few large ones. Split by behaviour, not by type-bucket.
- `main.go` is dumb: config → `app.Run(ctx, cfg)`. No logic.

---

## Go code style (the readability rules)

**Naming**
- Names say *what*, not *how*. `EstimateCost`, not `DoCalc`. `voyages`, not `data`/`list`.
- Short names for short scopes (`i`, `v` in a loop); descriptive for package-level.
- No stutter: `voyage.New`, not `voyage.NewVoyage`. No Hungarian/`I`-prefixed interfaces.

**Functions**
- **Small and single-purpose** (< 40 lines). One reason to change.
- **Max nesting depth 3.** Use early returns / guard clauses instead of `else` ladders.
- ≤ 3 params; if more, pass a struct.
- Pure where possible: domain logic takes inputs, returns outputs, no side effects.

**Immutability / value semantics** (your global rule, in Go)
- Don't mutate shared state. Return **new values** instead of mutating in place.
- Prefer value receivers for small structs; pointer receivers only when you must mutate or for large structs — and document why.
- No package-level mutable globals. Dependencies are injected, never reached for.

**Errors (comprehensive, explaining)**
- Always handle; never `_ =` an error silently.
- **Wrap with context:** `fmt.Errorf("create voyage %s: %w", id, err)`. Use `%w` so callers can `errors.Is/As`.
- Sentinel/typed errors in the domain (`var ErrVoyageNotFound = errors.New(...)`); adapters translate infra errors to domain errors.
- Validate at the boundary (gRPC handler / GraphQL resolver), so the domain can trust its inputs. Return clear messages; never leak internal/infra detail to the caller.

**Comments**
- Code says *what*; comments say *why*. No comment that restates the line.
- Exported symbols get a **godoc** sentence starting with the name. Add a short package doc.
- Mark intentional gaps with `// TODO(name): ...`, never silent.

**General**
- `gofmt`/`goimports` always. `golangci-lint` clean (it's a hard gate).
- No magic numbers/strings — name them as consts.
- Standard library first; add a dependency only when it clearly earns its place.

---

## Testing strategy (matches `./spec.md` layers)

**Shape of the pyramid (rebalanced for microservices):** ~60–70% unit, a strong service-integration layer, **contract tests** instead of broad E2E, and a *thin* E2E top. Push every test as far **down** as it can meaningfully live.

**Every gate ships with a red self-test.** A gate that has never been seen to fail is indistinguishable from a gate that can't (breaking-schema fixture, garbage Kafka message, `assert true` mutant test — each must be rejected, and CI checks that it is).

**L0 — Static.** golangci-lint, `buf lint/breaking`, gqlgen check, `graphql-inspector diff`. Hard gates on every PR.

**L0.5 — Mutation (`make mutate`).** go-mutesting (avito-tech fork, standalone in `./bin`) mutates the pure-domain packages and reruns `go test`: it verifies the *tests fail when the code breaks*, not just that they pass. Gotcha — this fork's labels are inverted: **PASS = mutant killed (good), FAIL = mutant survived (gap)**; the printed score is the kill rate. Floors are a **ratchet** set from measured baselines (equivalent mutants make 100% unreachable), enforced by `scripts/mutate.sh` — a drop means a test was weakened or new logic is untested; raise floors as coverage grows, never lower silently. Ships its red self-test (`--self-test`: a no-assertion suite must score low and be rejected), run first in CI.

**L1 — Unit (slim by scope decision, D-036).** Domain logic, no I/O. Run fast (`-short -race`). The calc golden-dataset story + one demo of each pattern; no coverage gate.
- **Table-driven, always**, with `t.Run(tc.name, …)` so failures name themselves.
- `require` for preconditions that must stop the test; `assert` for the checks you want all of.
- Fakes (hand-written small structs implementing a port) preferred over generated mocks for domain tests — they read better. Use **gomock** only in adapter tests where verifying call interactions matters (call made once, fields mapped, infra error → domain error).
- **Property tests are the explicit exception** to the no-randomness rule: always seeded (deterministic replay), asserting named invariants (monotonicity, non-negativity), in separate clearly-labelled functions next to the golden-dataset tables.

**L2 — Service integration.** One service, real DB/Kafka via **testcontainers-go**, downstream gRPC dep faked by **Gripmock**, driven by **Venom** YAML. Tag `//go:build integration`.
- **One shared fixture per package via TestMain**; `make test-svc` runs `-p 1` (one container set at a time — no boot storms).
- **Venom kafka steps:** `initialOffset: oldest`, unique `groupID` per testcase, step `retry`/`delay`. First step of every suite purges Gripmock stubs, then posts its own.
- **Assertions filter on `client_request_id`** — never counts, never `messages0` ordinals.

**L3 — Contract.** **Pact** (consumer-driven, brokerless: pact files + `PactFiles` verify in one CI job) for gRPC **and** async message pacts; **seeded** Schema Registry FULL-compat check; `buf breaking`; **graphql-inspector validate** over onboard-sync's committed `.graphql` ops (the lab's GraphQL consumer). This replaces most E2E. Every interface has a gate — if you add an edge, add its gate in the same PR.

**L4 — Whole-stack smoke.** **Venom HTTP/GraphQL** vs the compose stack, **1–2 journeys only**, zero stubs. Clean DB per run (`compose down -v` first), uniquely-named data per journey, retry-poll steps with an explicit documented budget. (Playwright UI E2E = out of lab scope, D-036.)

**L5 — Resilience.** **Toxiproxy** (timeout **and** reset_peer shapes) → assert disk-backed offline-queue + idempotent retry + version-counter conflict resolution (the vessel-sync story), including crash survival (abandon the live instance, cold-reopen the disk queue from a fresh one — in-process, no signals). Drive the client **in-process as a library**; assert via `SyncStatus()`, server rows, and scraped `/metrics` — never sleep-and-pray.

**L6 — Perf.** vegeta + ghz. Gate only on noise-immune signals (error rate, throughput floor, consumer-lag drain time); latency is trend-only. Never hard-gate p95 on shared runners.

**L7 — Shift-right.** "QA builds monitoring" (a common real-world pattern): the estimate-pipeline monitor — alert rules as code, unit-tested with `promtool test rules` incl. red fixtures; **absent series must ALERT** (no-data ≠ healthy); webhook alert route. Final layer — L8 (MCP/AI) removed by scope cut D-036.

**Test readability rules**
- A test is documentation. Name cases as behaviours: `"rejects negative distance"`, not `"test2"`.
- **Arrange / Act / Assert** with blank-line separation. One logical assertion focus per case.
- No logic in tests (no loops computing expected values) — write the expected value literally.
- Deterministic: no `time.Now()`/random in assertions; inject a clock, seed generators.

---

## North-star example (copy this shape)

```go
// domain/estimate.go — pure, no infra imports
package domain

// CostCalculator prices a voyage. Port: the domain declares what it needs.
type CostCalculator interface {
	Estimate(v Voyage) (Money, error)
}

// EstimateCost is pure logic: inputs in, value out, no side effects.
func EstimateCost(v Voyage, ratePerNm Money) (Money, error) {
	if v.DistanceNm < 0 {
		return Money{}, fmt.Errorf("estimate voyage %s: %w", v.ID, ErrNegativeDistance)
	}
	return ratePerNm.Mul(v.DistanceNm).Add(v.Fees), nil // returns a NEW value
}
```

```go
// domain/estimate_test.go — table-driven, self-naming, readable
func TestEstimateCost(t *testing.T) {
	tests := []struct {
		name    string
		voyage  Voyage
		rate    Money
		want    Money
		wantErr error
	}{
		{name: "prices distance plus fees", voyage: Voyage{DistanceNm: 100, Fees: usd(50)}, rate: usd(2), want: usd(250)},
		{name: "rejects negative distance", voyage: Voyage{DistanceNm: -1}, rate: usd(2), wantErr: ErrNegativeDistance},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := EstimateCost(tc.voyage, tc.rate)

			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
```

Why this is the model: domain logic is **pure** (trivially testable), the **port** is tiny and consumer-owned, the test is a **readable table**, and money is an **immutable value** (`Add`/`Mul` return new `Money`).

---

## GraphQL consumer contract (no front-end in this lab — D-036)

- The gateway and its resolvers are **Go/gqlgen**, governed by the Go rules above; boundary validation lives in gRPC handlers / GraphQL resolvers.
- The lab's GraphQL consumer is `clients/onboard-sync`: its operations live in committed `.graphql` files — the consumer contract corpus for `graphql-inspector validate`.
- Two tooling-only npm devDependencies: `@graphql-inspector/cli` (L3 GraphQL consumer-contract gate) and `squawk-cli` (DB DDL breaking-change gate, `make lint`). Root `package.json`, Node 22 via `.nvmrc`. No application JavaScript.

---

## Tooling & commands

```
make bootstrap        # one-shot dev setup (go tool directives + pact FFI + ghz + vegeta); CI runs it too
make doctor           # docker/testcontainers sanity (Colima/Podman socket checks)
make generate         # buf + gqlgen + avrogen → gen/ ; mockgen → adapter test pkgs (never gen/)
make lint             # golangci-lint + buf lint + buf breaking + squawk (migration breaking-change) + graphql-inspector diff
make migrate          # goose up (same embed.FS path app.Run uses)
make test-unit        # L1  go test ./... -short -race (slim layer, no coverage gate)
make mutate           # L0.5 go-mutesting over pure-domain pkgs; ratcheted kill-rate floors + red self-test
make test-svc         # L2  go test -tags=integration -p 1 ./...  (testcontainers + gripmock)
make venom            # L2  venom run test/venom/ --var-from-file=env.yml
make contract         # L3  pact (consumer → files → provider verify) + message pacts + seeded SR FULL check
make contract-graphql # L3  graphql-inspector validate (onboard-sync ops vs gateway SDL)
make smoke            # L4  compose --profile edge down -v && up -d --wait && venom run test/venom/smoke/
make resilience       # L5  toxiproxy scenarios (go test -tags=resilience)
make perf             # L6  vegeta + ghz + lag-drain check (franz-go admin API — no obs containers needed)
make ci               # THE gate (local; no CI server, D-036): lint + test-unit + mutate + test-svc + contract + contract-graphql
                      # run before every commit. smoke/resilience/perf = on-demand tier.
```

---

## Definition of Done (check before "done")

- [ ] Reads cleanly in one pass; names explain intent.
- [ ] Functions < 40 lines, nesting ≤ 3, files < 500 lines.
- [ ] Domain has **no** infra imports; ports are small and consumer-owned.
- [ ] Errors wrapped with `%w` and context; inputs validated at the boundary.
- [ ] No mutation of shared state; logic returns new values.
- [ ] Tests are table-driven, `t.Run`-named, deterministic, AAA-structured.
- [ ] `make lint` and `make test-unit` green; new behaviour has a test at the **lowest** sensible layer.
- [ ] New edge/interface → its contract gate lands in the same PR; new gate → its red self-test lands with it.
- [ ] Test assertions are identity-scoped (`client_request_id`) — no unscoped counts or ordinals (identity-scoped `count(*) = 1` IS the idempotency assertion; DLQ depth is the documented exception).
- [ ] No magic values, no dead code, no silent TODOs, no `console.log`.

---

## Notes for Claude (when generating code in this repo)

- **Show the port and the test alongside any new logic** — never logic without its fake-able interface and a table test.
- Prefer **hand-written fakes** in `domain` tests; reach for gomock only to assert interactions.
- Keep `main.go`/wiring thin; put logic in `domain`, I/O in `adapters`.
- Build features in the **phase order** of `./spec.md` (each phase compiles + tests green).
- If a change makes something hard to test, **stop and refactor the seam** (introduce a port) rather than writing an elaborate test.
- Default to the smallest change that reads well. Don't add abstraction "for later" — add it when a second caller appears.
