# Project: WES — Work Planning & Release (Core Bounded Context)

This service is the **core domain** of a Warehouse Execution System: it turns a
shift's **charge** (volume due by each deadline) into a **plan** (rate × heads
per process path), then **releases work continuously** (waveless) and performs
**flow balancing** using live buffer telemetry. It is the "conductor" — downstream
of WMS planning/inventory, upstream of WCS equipment control.

Source of truth for the domain model: the DDD reference at
`/Users/claudioed/docs/amazon-fulfillment-ddd.md` and
`/Users/claudioed/warehouse-systems-ddd.md`. Honor the ubiquitous language there.

## Architecture (NON-NEGOTIABLE)

Hexagonal / Ports & Adapters. Strict dependency rule: **domain depends on
nothing; application depends on domain; adapters depend on application/domain.**
No framework or SQL types in the domain layer.

```
cmd/wes/                     main.go — wiring/composition root
internal/
  domain/                    pure Go: aggregates, value objects, domain events, domain errors
    charge/                  ChargeForecast aggregate
    plan/                    ShiftPlan, PathPlan aggregates
    release/                 WorkPool aggregate, release policy (domain service)
    workunit/                WorkUnit aggregate
    shared/                  value objects: CPT, Rate, PathId, Quantity, events
  application/               use cases (ports IN) + port interfaces (ports OUT)
    ports/                   driven-port interfaces (repositories, event publisher, clock)
    usecases/                one struct per use case, orchestrates domain
  adapters/
    inbound/http/            REST handlers (chi or net/http), DTOs, mapping
    outbound/postgres/       repository impls (pgx), migrations
    outbound/memory/         in-memory repo impls for tests/local
    outbound/events/         event publisher (log/in-memory now; kafka-ready iface)
migrations/                  golang-migrate SQL files
```

## Ubiquitous Language (use these exact names)

- **Charge** — volume that must clear, bucketed by CPT. Not "total due today".
- **CPT (Critical Pull Time)** — last moment a parcel can be manifested and still
  make its truck. A value object on work; priority derives from it.
- **Process Path** — a named station that owns a QUEUE (not a workflow step):
  unit-in → unit-out, direct/indirect, a service rate, a staffed capacity.
- **Work Pool** — the queue for one process path: backlog depth, arrival rate,
  service rate. Release-fed (WES controls volume) vs flow-fed (priority only).
- **WorkUnit** — a releasable unit of work (e.g. a pick task). Assigned at most
  once at a time; cannot complete twice.
- **ShiftPlan / PathPlan** — committed split of headcount across paths: rate ×
  heads × hours per path. Invariant: plannedHeads ≤ installedStations.
- **Release** — continuous, priority-ordered admission of work into a pool
  (waveless). The release decision is a POLICY object, not a schedule.
- **Flow balancing** — on telemetry (backlog vs plan): throttle upstream release
  or flag labor reassignment. Drum-Buffer-Rope with CPT as the drum.

## Aggregates & invariants (enforce in domain, unit-tested)

- **WorkPool**: hands out work in priority order, at most once. WIP limit is an
  enforceable invariant on release-fed pools; an alarm threshold on flow-fed.
- **WorkUnit**: at most one active assignment; no double-complete; carries CPT.
- **ShiftPlan**: plannedHeads(path) ≤ installedStations(path); sum of hours valid.
- Read models (backlog depth, actual rate, plan-vs-actual) are PROJECTIONS built
  from events — NOT state on aggregates.

## Domain events (past tense)

ChargeForecastReceived, ShiftPlanCommitted, WorkUnitCreated, WorkReleased,
BacklogThresholdBreached, RateDeviationDetected, PathThrottled,
LaborReassignmentFlagged, WorkUnitCompleted.

## Use cases (application layer)

1. ReceiveChargeForecast(path, cptBuckets) → ChargeForecast
2. CommitShiftPlan(path, heads, rate, hours) → ShiftPlan (validates invariant)
3. EnqueueWorkUnit(path, cpt, ref) → WorkUnit added to pool
4. ReleaseNextWork(path) → applies release policy, returns released unit(s)
5. RecordCompletion(workUnitId) → WorkUnitCompleted, updates telemetry
6. SampleBacklog(path) → returns depth/rate read model; may raise threshold events
7. RebalanceDecision(path) → throttle vs reassign recommendation

## REST API (inbound adapter)

- POST /paths/{pathId}/charge            → ReceiveChargeForecast
- POST /paths/{pathId}/plan              → CommitShiftPlan
- POST /paths/{pathId}/work-units        → EnqueueWorkUnit
- POST /paths/{pathId}/release           → ReleaseNextWork
- POST /work-units/{id}/complete         → RecordCompletion
- GET  /paths/{pathId}/telemetry         → SampleBacklog read model
- GET  /paths/{pathId}/rebalance         → RebalanceDecision
- GET  /healthz

JSON DTOs live in the http adapter; never leak domain structs directly.

## Tech & standards

- Go 1.26, modules. Module path: `github.com/claudioed/wes-work-planning`.
- Router: `chi` (github.com/go-chi/chi/v5). DB: `pgx/v5` + `pgxpool`.
  Migrations: `golang-migrate` SQL files under migrations/.
- Config via env (DATABASE_URL, HTTP_ADDR). Provide a docker-compose.yml for Postgres.
- Errors: domain returns typed errors; adapters map to HTTP status.
- Tests: table-driven. Domain + application unit tests with the in-memory adapter.
  At least one httptest integration test per endpoint against in-memory repos.
  Postgres repo has a build-tagged integration test (skipped without DATABASE_URL).
- `gofmt`/`go vet` clean. Every package has a short doc comment.

## Definition of done

- `go build ./...` and `go test ./...` both green (unit + httptest layers).
- `go vet ./...` clean.
- README.md: how to run (compose up, migrate, go run), the endpoints with curl
  examples, and a short note on the hexagonal layering.
- The three named aggregate invariants are each covered by a failing-path test.

---

## Cross-service integration (additive — Task 7, do NOT touch existing domain code)

This service both publishes and consumes integration events over Kafka. This is
strictly additive: new adapters and a new read-model projection. Do not modify
any existing aggregate, invariant, or use case from the sections above.

### Envelope (identical across all four warehouse-systems services)

```json
{
  "event_id": "uuid-v4",
  "event_type": "WorkReleased",
  "occurred_at": "2026-08-21T22:00:00Z",
  "source": "wes-work-planning",
  "data": { }
}
```

`event_id` is a UUID v4 generated at publish time. `source` is always this
service's own name. `data` is event-type-specific (schemas below).

### Kafka

- Client library: `github.com/segmentio/kafka-go` (pure Go, no cgo).
- Broker: `KAFKA_BROKERS` env var (default `localhost:9092`), comma-separated.
- New adapter package: `internal/adapters/outbound/kafka/` implementing the
  existing `ports.EventPublisher` interface (`Publish(ctx, events...) error`) —
  it serializes each domain event into the envelope above and writes it to this
  service's own topic. Keep the existing log/memory publisher too; select via
  env (`EVENT_PUBLISHER=kafka|log`, default `log` so existing tests are
  unaffected).
- New inbound package: `internal/adapters/inbound/kafka/` — a consumer that
  reads from topic(s) this service subscribes to (below) and calls a new,
  additive application-layer projector use case per consumed event type.

### What this service PUBLISHES

Topic: `warehouse.work-planning.events`

- `WorkReleased` — publish when `ReleaseNextWork` releases a unit.
  `data`: `{"path_id": "...", "work_unit_id": "...", "cpt": "RFC3339", "ref": "..."}`
  Downstream consumer: fulfillment-execution turns this into a Task.

### What this service CONSUMES

1. Topic `warehouse.workforce.events`, event_type `ShiftPlanCommitted`.
   `data`: `{"building_id","shift_id","path_id","planned_heads","planned_rate","planned_hours"}`.

   IMPORTANT — do not feed this into this service's own `ShiftPlan`/`PathPlan`
   aggregate or its `CommitShiftPlan` use case. That aggregate represents THIS
   service's own committed plan and is a DIFFERENT model from Workforce
   Management's `ShiftPlan` even though the word is the same (classic DDD
   "same term, different bounded context" — do not conflate them). Instead:
   add a new read-only projection, `LaborPlanObserved` (new package
   `internal/domain/laborview/` — a plain value/read-model type, not an
   aggregate with invariants), keyed by `path_id`, storing the latest observed
   `planned_heads/rate/hours` from Workforce. Persist it via a new
   `LaborPlanViewRepo` port + memory/postgres adapters (new table
   `labor_plan_view`). Expose it read-only via `GET /paths/{pathId}/labor-plan-view`
   and make `RebalanceDecision` optionally include it as extra context in its
   response (additive field, do not change its existing decision logic/tests).

2. Topic `warehouse.inventory.events`, event_types `StockReserved`,
   `ReservationRevoked`.
   `data` for both: `{"sku": "...", "quantity": N, "demand_ref": "..."}`.

   Project into a new read-model `UsableInventoryObserved` keyed by SKU (NOT by
   path — Inventory reservations are SKU-scoped, not path-scoped; do not force
   a path mapping that does not exist). `StockReserved` decrements the observed
   usable count for that SKU, `ReservationRevoked` increments it back. New
   package `internal/domain/inventoryview/`, new port + memory/postgres
   adapters (new table `usable_inventory_view`). Expose read-only via
   `GET /inventory-view/{sku}`.

### Idempotency (required — Kafka is at-least-once)

Every consumer path MUST be idempotent under redelivery. Add a new Postgres
table `processed_events (event_id TEXT PRIMARY KEY, processed_at TIMESTAMPTZ)`
via a new migration. Before applying a consumed event's effect, attempt to
insert its `event_id`; if the insert violates the primary key (already
processed), skip processing and ack/commit the message anyway. For the
in-memory adapter, use a thread-safe `map[string]struct{}` for the same
purpose. Unit-test: publishing the same event twice must not double-decrement
`UsableInventoryObserved` or double-write `LaborPlanObserved`.

### Local integration testing

- docker-compose.yml gains a `kafka` service reference is NOT needed here (a
  shared broker already runs at `~/warehouse-systems/docker-compose.kafka.yml`
  on `localhost:9092` — connect to it, don't spin up your own broker).
- Add a build-tagged integration test (`//go:build integration`) that publishes
  a `ShiftPlanCommitted`-shaped and a `StockReserved`-shaped message to the real
  broker and asserts the read-models update, skipped without `KAFKA_BROKERS` set.

### Definition of done for Task 7

- New adapters compile and unit-test green with the existing full suite still
  green (`go build ./...`, `go vet ./...`, `go test ./...`, `go test ./... -race`).
- Idempotency test passes (duplicate event = no double effect).
- README gains an "Integration" section: topics published/consumed, the exact
  JSON schemas above, the env vars (`KAFKA_BROKERS`, `EVENT_PUBLISHER`), and how
  to smoke-test against the shared broker with `kafka-console-producer`.
- Do a REAL smoke test: with the shared Kafka broker running, actually publish
  a `ShiftPlanCommitted` message via `kafka-console-producer.sh` (or a small Go
  one-off) and curl `GET /paths/{pathId}/labor-plan-view` to see it reflected.
