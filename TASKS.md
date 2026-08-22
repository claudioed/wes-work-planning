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

## Task 7 — Cross-service integration (additive, see CLAUDE.md's new section)
- Add `github.com/segmentio/kafka-go` dependency.
- New Kafka outbound publisher adapter (publishes WorkReleased) selected via
  EVENT_PUBLISHER env, default stays "log" so nothing existing breaks.
- New Kafka inbound consumer for ShiftPlanCommitted (from workforce) and
  StockReserved/ReservationRevoked (from inventory).
- New read-model packages: internal/domain/laborview (by path_id),
  internal/domain/inventoryview (by sku). New repos/ports/adapters (memory +
  postgres) for each, new migration for processed_events + the two view tables.
- New GET endpoints: /paths/{pathId}/labor-plan-view, /inventory-view/{sku}.
- Idempotency via processed_events table (Postgres) / map (memory); unit test
  double-delivery has no double effect.
- Build-tagged (`integration`) test against the shared broker at localhost:9092
  (docker-compose.kafka.yml in ~/warehouse-systems), skipped w/o KAFKA_BROKERS.
- README gains an Integration section. Do a REAL smoke test against the running
  shared broker before declaring done (publish a message, curl the view endpoint).
- Full existing suite (build/vet/test/-race) must still be green afterward.

## Task 8 — Consume TaskCompleted (additive, see CLAUDE.md's Task 8 section)
- Add a THIRD topic subscription to the existing Task 7 Kafka consumer:
  warehouse.fulfillment.events, filtering event_type "TaskCompleted".
- Map data.work_unit_id -> RecordCompletionRequest.WorkUnitId and call the
  existing RecordCompletion use case directly. Do not modify RecordCompletion.
- Reuse the existing processed_events idempotency mechanism from Task 7.
- Unit test: fake TaskCompleted envelope invokes RecordCompletion once;
  redelivery of the same event_id does not invoke it again.
- README's Integration section gains this new topic. REAL smoke test: with the
  shared broker running, publish a TaskCompleted message for a work unit
  currently in Released state and confirm it transitions to Completed.
- Full existing suite (build/vet/test/-race), including Tasks 0-7, must stay green.

## Task 10 — Quality engineering: linting, coverage, integration tests, mutation tests, CI
Full spec in QUALITY.md at the repo root. Five ordered stages, each gates the
next: (1) golangci-lint clean via the committed .golangci.yml, (2) unit test
coverage >= 90% on internal/domain/... + internal/application/... combined,
(3) real integration tests against live Postgres for every outbound Postgres
adapter, (4) gremlins mutation testing on internal/domain/... only
(exploratory, triaged not gated), (5) .github/workflows/ci.yml — lint+unit+
integration blocking on every push/PR, mutation testing on a weekly schedule/
manual dispatch only, never blocking PRs. Do not stop until every stage's
Definition of Done in QUALITY.md is met, then report the final numbers.
