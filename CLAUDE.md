# Project: WES — Work Planning & Release (Core Bounded Context)

> **Study project.** Educational DDD exercise using real WMS/WES/WCS
> terminology (waveless release, CloudEvents, RFC 7807, hexagonal
> architecture). Not a production system, not affiliated with Amazon or any
> company.

This service is the **core domain** of a Warehouse Execution System: it turns
a shift's **charge** (volume due by each deadline) into a **plan** (rate ×
heads per process path), **releases work continuously** (waveless), and
performs **flow balancing** using live buffer telemetry. It is the fleet's
"conductor" — downstream of WMS planning/inventory, upstream of WCS equipment
control.

Source of truth for the domain model: the DDD reference at
`/Users/claudioed/docs/amazon-fulfillment-ddd.md` and
`/Users/claudioed/warehouse-systems-ddd.md`. Honor the ubiquitous language
defined there and in `.claude/rules/domain-model.md`.

## Project Overview

- Module: `github.com/claudioed/wes-work-planning`, Go 1.26.6.
- Three deployable Go binaries (`cmd/wes`, `cmd/wes-projector`,
  `cmd/wes-reports`) plus an MCP server (`cmd/mcp`) and a standalone frontend
  micro-frontend (`web/`, Vite + React Module Federation remote).
- GitFlow: `develop` is the default working branch, `main` is release-only.
- Contracts are spec-first: `apis/openapi.yaml` (REST, Spectral-linted) and
  `apis/asyncapi.yaml` (Kafka events, Spectral-linted) are the sources of
  truth — the Docusaurus site under `docs/` **generates** its REST reference
  pages from `apis/openapi.yaml` at build time (`docs/docs/api/rest/*.api.mdx`,
  via the `prebuild` npm script); never hand-edit those generated files.

## Architecture (NON-NEGOTIABLE)

Hexagonal / Ports & Adapters. Strict dependency rule: **domain depends on
nothing; application depends on domain; adapters depend on
application/domain.** No framework or SQL types in the domain layer.

Full layer-by-layer directory map, the additive analytics data product
(ADR-0011), and the `web/` micro-frontend boundary are documented in
**`.claude/rules/architecture.md`** — read it before touching any adapter or
adding a new bounded-context boundary.

## Key Commands

```sh
# Run in-memory (no infra)
go run ./cmd/wes                       # :8080

# Run against Postgres
docker compose up -d
migrate -path migrations -database "$DATABASE_URL" up
DATABASE_URL="postgres://wes:***@localhost:5432/wes?sslmode=disable" go run ./cmd/wes

# Analytics data product (separate DB + Kafka fan-out)
go run ./cmd/wes-projector              # :8091 — the ONLY writer
go run ./cmd/wes-reports                # :8092 — read-only reports API

# Docs site (Docusaurus) — regenerates REST reference from apis/openapi.yaml
cd docs && npm ci && npm run build      # prebuild hook runs clean-api-docs + gen-api-docs
```

Local quality gate — **run before every commit**:

```sh
make check       # fmt-check, vet, build, lint, test -race — fast, run every change
make check-all   # + coverage gate (90%), arch-test, bdd — run before pushing
make vuln        # govulncheck — run when touching go.mod
make mutation    # fast blocking mutation subset (CI-enforced thresholds in .gremlins.yaml)
```

`lefthook install` wires these into git hooks (pre-commit: fmt-check/vet/lint;
pre-push: `make check`), but proactively run `make check` yourself rather than
relying on the hook firing.

## Code Standards / Testing

- Router: `chi` (`go-chi/chi/v5`). DB: `pgx/v5` + `pgxpool`. Migrations:
  `golang-migrate` SQL files under `migrations/` (OLTP) and
  `migrations/analytics/` (analytics store, applied only by `cmd/wes-projector`).
- Errors: domain returns typed errors; the HTTP adapter maps them to RFC 7807
  `application/problem+json` responses (ADR-0005) — never a bespoke error
  shape.
- Tests: table-driven. Domain + application unit tests use the in-memory
  adapter. At least one httptest integration test per endpoint. The Postgres
  repo has a build-tagged (`//go:build integration`) test, skipped without
  `DATABASE_URL`. Kafka-touching integration tests MUST use testcontainers
  (`github.com/testcontainers/testcontainers-go/modules/kafka`), never a
  skip-gated `KAFKA_BROKERS` check against an external broker — CI's
  `integration` job has no Kafka service, so a skip-gated test silently
  proves nothing there.
- `gofmt`/`go vet` clean. Every package has a short doc comment.
- Definition of done for any change: `go build ./...`, `go test ./...`
  (unit + httptest), `go vet ./...` all green; README updated if the change
  touches run instructions, env vars, or the API surface; any new/changed
  aggregate invariant has a failing-path test.

## Domain Model

Ubiquitous language, the four aggregates and their enforced invariants, the
nine domain events, and the seven core use cases are documented in
**`.claude/rules/domain-model.md`** — read it before writing any use case or
domain logic. Use those exact terms; do not invent synonyms.

## REST API & Cross-Service Integration

- REST surface (11 operations across health/charge/plan/work-units/release/
  telemetry/rebalance/labor-plan-view/inventory-view), the Kafka integration
  event contract (published + consumed topics, envelope, idempotency), and
  the read-only projections built from consumed events (`LaborPlanObserved`,
  `UsableInventoryObserved`) are documented in
  **`.claude/rules/integration-events.md`**.
- Full request/response schemas, every status code, and the shared `Problem`
  error component live in [`apis/openapi.yaml`](./apis/openapi.yaml) — this
  is the spec of record; the Docusaurus REST reference is generated from it,
  never edited by hand.
- Async contract of record is [`apis/asyncapi.yaml`](./apis/asyncapi.yaml)
  (CloudEvents 1.0 over Kafka); narrative pages in `docs/docs/api/events.md`
  and `docs/docs/ecosystem/integration-events.md` are written from it and
  should be updated by hand whenever a message/channel changes there (this
  fleet documents AsyncAPI narratively per-service; there is no generated
  AsyncAPI static site in this repo — that only exists in the separate
  fleet-wide docs aggregator).

## Architecture Decision Records

16 ADRs under `docs/docs/adr/` cover hexagonal layering (0001), waveless
release (0002), flow balancing (0003), Kafka integration events (0004),
RFC 7807 (0005), the Labor Plan View vs. Workforce's own ShiftPlan model
distinction (0006), Go architecture fitness tests (0007), the MCP inbound
adapter (0008), product classification propagation (0009), gift-wrap as a
`WorkReleased` characteristic (0010), the analytics data product (0011),
process-path catalogue validation (0012), the standard metrics convention
(0013), the transactional outbox (0014), and the two REST-identity /
static-bearer-auth ADRs (0015 added it, 0016 records its fleet-wide removal).
Read the relevant ADR before reversing a documented decision.
