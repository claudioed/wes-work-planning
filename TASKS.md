# Build Tasks — WES Work Planning & Release

Build the full bounded context described in CLAUDE.md. Work in this order; keep
`go build ./...` and `go test ./...` green as you go. Read
/Users/claudioed/docs/amazon-fulfillment-ddd.md for the domain model before coding.

## Task 0 — Project skeleton
- `go mod init github.com/claudioed/wes-work-planning`
- Create the directory layout from CLAUDE.md. Add .gitignore (bin/, .env).
- Add deps: chi/v5, pgx/v5, golang-migrate (CLI usage only in README).

## Task 1 — Domain layer (pure Go, no imports outside stdlib)
- shared: value objects CPT (time-based, ordering), Rate (units/hour), PathId,
  Quantity, StationCount; a DomainEvent type + concrete events (see CLAUDE.md).
- charge: ChargeForecast aggregate (CPT buckets → quantities).
- plan: PathPlan + ShiftPlan aggregates. Enforce plannedHeads ≤ installedStations.
- workunit: WorkUnit aggregate — states (Pending→Released→Completed), carries CPT,
  at-most-once assignment, no double-complete.
- release: WorkPool aggregate (priority by CPT, at-most-once handout, WIP limit
  invariant on release-fed pools) + a ReleasePolicy domain service.
- Unit tests for EVERY invariant, including the failing paths.

## Task 2 — Application layer
- ports: define OUT interfaces — ChargeRepo, PlanRepo, WorkPoolRepo,
  WorkUnitRepo, EventPublisher, Clock.
- usecases: one struct per use case in CLAUDE.md (ReceiveChargeForecast,
  CommitShiftPlan, EnqueueWorkUnit, ReleaseNextWork, RecordCompletion,
  SampleBacklog, RebalanceDecision). Depend only on domain + ports.
- Unit-test use cases against the in-memory adapter.

## Task 3 — Outbound adapters
- memory: in-memory implementations of every port (thread-safe).
- postgres: pgxpool-backed repos + migrations (charge, plan, work_pool,
  work_unit, events tables). Build-tag integration tests (skip w/o DATABASE_URL).
- events: an EventPublisher that logs + buffers; leave a kafka-ready interface.

## Task 4 — Inbound HTTP adapter
- chi router, handlers for every endpoint in CLAUDE.md, request/response DTOs,
  domain-error → HTTP status mapping, /healthz.
- httptest integration tests per endpoint wired to in-memory repos.

## Task 5 — Composition root & ops
- cmd/wes/main.go wires config (env) → adapters → use cases → router.
- docker-compose.yml (Postgres 16). README.md with run steps + curl examples.

## Task 6 — Verify
- `go build ./...`, `go vet ./...`, `go test ./...` all green. Fix until they are.
- Confirm the three invariants each have a red-path test.
