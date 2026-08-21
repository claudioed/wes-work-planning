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
