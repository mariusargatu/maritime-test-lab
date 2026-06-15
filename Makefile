# maritime-test-lab — daily verbs. THE gate is `make ci` (run before every commit;
# no GitHub Actions, D-036). Layer targets are phase-aware: each guards on the
# files its phase creates and prints a skip notice until then, so `make ci` is
# green from Phase 0 onward (Q5).

GO  ?= go
BUF  = $(GO) tool buf
LINT = $(GO) tool golangci-lint
AVROGEN = $(GO) tool avrogen
GQLGEN = $(GO) tool gqlgen
# venom is installed standalone into ./bin, NOT a go.mod tool directive: its
# protoreflect dependency clashes with buf in a shared module graph (D-040).
VENOM = ./bin/venom
VENOM_VERSION = v1.3.0
GRIPMOCK_IMAGE = bavix/gripmock:3.13.1
PROM_IMAGE = prom/prometheus:v3.2.0
# Pact uses CGO + the FFI installed to ~/.pact/lib (not /usr/local/lib — unwritable
# without sudo). The protobuf plugin is a separate one-time install (D-043).
PACT_LIBDIR = $(HOME)/.pact/lib
PACT_ENV = DYLD_LIBRARY_PATH=$(PACT_LIBDIR) CGO_ENABLED=1 CGO_LDFLAGS=-L$(PACT_LIBDIR)
PACT_GO_VERSION = v2.5.1
# go-mutesting (avito-tech fork) drives the L0.5 mutation gate. Installed standalone
# into ./bin, NOT a go.mod tool directive — its dep graph would bloat the shared
# module (same reasoning as venom, D-040). Pinned for reproducible scores.
GOMUTESTING = ./bin/go-mutesting
GOMUTESTING_VERSION = v0.0.0-20251226130216-48d0401f00fb

# Compose verbs. `up` = core substrate (pg+redpanda+voyage+estimator); the demo
# layers are profile-gated. `down` removes ALL project containers regardless of
# profile, so it is the single clean-slate teardown (-v drops volumes too).
COMPOSE      = docker compose
COMPOSE_ALL  = docker compose --profile edge --profile chaos --profile obs

.DEFAULT_GOAL := help

.PHONY: help bootstrap doctor generate lint migrate \
        up up-edge up-all down logs ps \
        test-unit test-svc venom contract contract-graphql \
        mutate smoke resilience perf ci

help: ## list targets
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN{FS=":.*?## "}{printf "  %-18s %s\n", $$1, $$2}'

bootstrap: ## one-shot dev setup (go deps + npm + standalone CLIs)
	$(GO) mod download
	npm ci
	@mkdir -p bin
	GOBIN="$(CURDIR)/bin" $(GO) install github.com/ovh/venom/cmd/venom@$(VENOM_VERSION)
	GOBIN="$(CURDIR)/bin" $(GO) install github.com/pact-foundation/pact-go/v2@$(PACT_GO_VERSION)
	GOBIN="$(CURDIR)/bin" $(GO) install github.com/bojand/ghz/cmd/ghz@latest
	GOBIN="$(CURDIR)/bin" $(GO) install github.com/tsenart/vegeta/v12@latest
	GOBIN="$(CURDIR)/bin" $(GO) install github.com/avito-tech/go-mutesting/cmd/go-mutesting@$(GOMUTESTING_VERSION)
	./bin/pact-go install --libDir "$(PACT_LIBDIR)" || echo "bootstrap: pact FFI install needs network/perms — see scripts/install-pact-plugin.sh"
	@echo "bootstrap: go tools pinned in go.mod; venom + pact-go + ghz + vegeta installed to ./bin (Go-native load tools, D-048)."
	@echo "bootstrap: run scripts/install-pact-plugin.sh once for the gRPC pact (external binary)."

doctor: ## docker / container sanity
	@docker context show
	@docker info >/dev/null 2>&1 && echo "doctor: docker daemon reachable" \
		|| { echo "doctor: docker daemon NOT reachable"; exit 1; }
	@docker run --rm hello-world >/dev/null 2>&1 && echo "doctor: container run OK" \
		|| { echo "doctor: cannot run containers"; exit 1; }
	@docker image inspect $(GRIPMOCK_IMAGE) >/dev/null 2>&1 \
		&& echo "doctor: $(GRIPMOCK_IMAGE) present" \
		|| { echo "doctor: pulling $(GRIPMOCK_IMAGE)"; docker pull $(GRIPMOCK_IMAGE) >/dev/null; }
	@ls $(PACT_LIBDIR)/libpact_ffi.* >/dev/null 2>&1 \
		&& echo "doctor: pact FFI present" || echo "doctor: pact FFI missing — make bootstrap"
	@ls -d $(HOME)/.pact/plugins/protobuf-* >/dev/null 2>&1 \
		&& echo "doctor: pact protobuf plugin present" || echo "doctor: pact protobuf plugin missing — scripts/install-pact-plugin.sh"

generate: ## buf + avrogen + gqlgen -> gen/
	$(BUF) generate
	$(AVROGEN) -pkg avro -strict-types -o gen/avro/voyage_created.go schemas/voyage_created.avsc
	$(AVROGEN) -pkg avro -strict-types -o gen/avro/estimate_ready.go schemas/estimate_ready.avsc
	$(GQLGEN) generate --config gateway/gqlgen.yml

lint: ## L0 golangci-lint + buf lint/breaking + squawk migration breaking-change (+ graphql-inspector diff in P5)
	$(LINT) run ./...
	$(BUF) lint
	$(BUF) breaking --against '.git#branch=main'
	@if [ -f scripts/lint-migrations.sh ]; then \
		./scripts/lint-migrations.sh ; \
	else echo "skip db-ddl breaking gate (scripts/lint-migrations.sh — Phase 0)" ; fi
	@if [ -f gateway/gqlgen.yml ]; then \
		$(GQLGEN) generate --config gateway/gqlgen.yml >/dev/null && git diff --exit-code gen/graphql gateway/graph \
			|| { echo "gqlgen output is stale — run 'make generate' and commit"; exit 1; } ; \
	else echo "skip gqlgen regen check (gateway arrives in Phase 5)" ; fi
	@if ls schemas/*.avsc >/dev/null 2>&1; then \
		$(AVROGEN) -pkg avro -strict-types -o gen/avro/voyage_created.go schemas/voyage_created.avsc && \
		$(AVROGEN) -pkg avro -strict-types -o gen/avro/estimate_ready.go schemas/estimate_ready.avsc && \
		git diff --exit-code gen/avro \
			|| { echo "gen/avro is stale — run 'make generate' and commit"; exit 1; } ; \
	else echo "skip avro regen check (schemas arrive in Phase 0)" ; fi
	@if [ -f gateway/schema.graphqls ]; then \
		npx --no-install graphql-inspector diff "git:main:gateway/schema.graphqls" gateway/schema.graphqls ; \
	else echo "skip graphql-inspector diff (gateway SDL arrives in Phase 5)" ; fi
	@if [ -d observability/rules ]; then \
		docker run --rm -v "$(CURDIR)/observability:/o" --entrypoint sh $(PROM_IMAGE) -c 'promtool test rules /o/rules/tests/*.yaml' ; \
	else echo "skip promtool test rules (monitor arrives in Phase 7)" ; fi

migrate: ## apply goose migrations (same embed.FS app.Run uses)
	$(GO) run ./services/voyage/cmd/migrate

up: ## bring CORE infra up (pg + redpanda + voyage + estimator)
	$(COMPOSE) up -d --wait --build

up-edge: ## core + gateway (the GraphQL edge — what smoke/L5/L6 need)
	$(COMPOSE) --profile edge up -d --wait --build

up-all: ## EVERYTHING up (edge + chaos + obs profiles)
	$(COMPOSE_ALL) up -d --wait --build

down: ## stop ALL containers (every profile) + drop volumes — clean slate
	$(COMPOSE_ALL) down -v

logs: ## tail logs of all running services (Ctrl-C to stop)
	$(COMPOSE_ALL) logs -f

ps: ## show stack status across all profiles
	$(COMPOSE_ALL) ps

test-unit: ## L1 go test -short -race (slim layer, no coverage gate)
	$(GO) test ./... -short -race

mutate: ## L0.5 mutation gate over pure-domain pkgs (go-mutesting; ratcheted kill-rate floors)
	@if [ -x "$(GOMUTESTING)" ]; then \
		./scripts/mutate.sh --self-test && ./scripts/mutate.sh ; \
	else echo "mutate: $(GOMUTESTING) missing — run 'make bootstrap'"; exit 1; fi

test-svc: ## L2 testcontainers + gripmock + venom (Phase 2)
	@if [ -d test/venom ]; then $(GO) test -tags=integration -race -p 1 ./... ; \
	else echo "skip test-svc / L2 (arrives Phase 2)"; fi

venom: ## L2 venom suites (Phase 2)
	@if [ -d test/venom ]; then $(GO) test -tags=integration -p 1 ./test/venom/... ; \
	else echo "skip venom / L2 (arrives Phase 2)"; fi

contract: ## L3 seeded SR FULL-compat + red fixture + brokerless pacts
	@if [ -d test/contract ]; then $(GO) test -tags=contract -count=1 ./test/contract/... ; \
	else echo "skip contract / L3 (arrives Phase 3)"; fi
	@if [ -d test/pact ]; then \
		$(PACT_ENV) $(GO) test -tags=pact -count=1 -run Consumer ./test/pact/... && \
		$(PACT_ENV) $(GO) test -tags=pact -count=1 -run Provider ./test/pact/... ; \
	else echo "skip pact / L3 (arrives Phase 4)"; fi

contract-graphql: ## L3 graphql-inspector validate onboard-sync ops (Phase 5)
	@if [ -d clients/onboard-sync ] && [ -f gateway/schema.graphqls ]; then \
		npx --no-install graphql-inspector validate "clients/onboard-sync/**/*.graphql" gateway/schema.graphqls --deprecated ; \
	else echo "skip contract-graphql / L3 (arrives Phase 5)"; fi

smoke: ## L4 whole-stack venom vs compose (Phase 5)
	@if [ -d test/venom/smoke ]; then \
		docker compose --profile edge down -v && docker compose --profile edge up -d --wait --build && \
		$(VENOM) run test/venom/smoke/ \
			--var gateway_url=http://localhost:$${GATEWAY_PORT:-18080} \
			--var uuid_flagship=$$(uuidgen) --var uuid_dup=$$(uuidgen) ; \
	else echo "skip smoke / L4 (arrives Phase 5)"; fi

resilience: ## L5 toxiproxy scenarios (Phase 6b)
	@if [ -d test/resilience ]; then \
		docker compose --profile edge --profile chaos up -d --wait --build && \
		$(GO) test -tags=resilience -count=1 ./test/resilience/... ; \
	else echo "skip resilience / L5 (arrives Phase 6b)"; fi

perf: ## L6 lag-drain + error-rate gate (Phase 7)
	@if [ -d test/perf ]; then \
		docker compose --profile edge up -d --wait --build && \
		$(GO) test -tags=perf -count=1 ./test/perf/... ; \
	else echo "skip perf / L6 (arrives Phase 7)"; fi

ci: lint test-unit mutate test-svc contract contract-graphql ## THE gate (run before every commit)
	@echo "ci: green"
