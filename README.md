# WES — Work Planning & Release

The core bounded context of a Warehouse Execution System. It turns a shift's
**charge** (volume due by each CPT) into a **plan** (rate x heads per process
path), releases work continuously in priority order (waveless), and performs
flow balancing off live backlog telemetry — Drum-Buffer-Rope with CPT as the
drum.

## Architecture

Hexagonal / Ports & Adapters, with a strict dependency rule: **domain depends
on nothing; application depends on domain; adapters depend on
application/domain.**

```
cmd/wes/                     main.go — wiring/composition root
internal/
  domain/                    pure Go: aggregates, value objects, domain events
    charge/                  ChargeForecast aggregate
    plan/                    ShiftPlan, PathPlan aggregates
    release/                 WorkPool aggregate, ReleasePolicy domain service
    workunit/                WorkUnit aggregate
    shared/                  value objects (CPT, Rate, PathId, Quantity, ...)
  application/                use cases (ports IN) + port interfaces (ports OUT)
    ports/                   driven-port interfaces
    usecases/                one struct per use case
  adapters/
    inbound/http/            REST handlers, DTOs, chi router
    outbound/postgres/       pgx repositories + migrations
    outbound/memory/         in-memory repositories (tests/local)
    outbound/events/         log-based event publisher (Kafka-ready interface)
migrations/                  golang-migrate SQL files
```

The domain package imports nothing outside the Go standard library. The
application layer depends only on domain types and the `ports` interfaces it
defines. Adapters are the only layer allowed to import a framework, a SQL
driver, or an HTTP router.

## Running

### Option 1 — in-memory (no database)

```sh
go run ./cmd/wes
```

The server starts on `:8080` using in-memory repositories (state is lost on
restart) — good for trying the API without any infrastructure.

### Option 2 — Postgres

```sh
docker compose up -d

# apply migrations (requires the golang-migrate CLI: https://github.com/golang-migrate/migrate)
migrate -path migrations -database "postgres://wes:wes@localhost:5432/wes?sslmode=disable" up

DATABASE_URL="postgres://wes:wes@localhost:5432/wes?sslmode=disable" go run ./cmd/wes
```

### Config

| Env var        | Default | Purpose                                    |
|----------------|---------|---------------------------------------------|
| `HTTP_ADDR`    | `:8080` | Address the HTTP server listens on          |
| `DATABASE_URL` | (unset) | Postgres DSN; falls back to in-memory if unset |

## API

All bodies/responses are JSON. Timestamps are RFC3339.

### `POST /paths/{pathId}/charge` — ReceiveChargeForecast

```sh
curl -X POST localhost:8080/paths/pick-a/charge \
  -H 'Content-Type: application/json' \
  -d '{"buckets":[{"cpt":"2026-08-21T12:00:00Z","quantity":100}]}'
```

### `POST /paths/{pathId}/plan` — CommitShiftPlan

```sh
curl -X POST localhost:8080/paths/pick-a/plan \
  -H 'Content-Type: application/json' \
  -d '{"plannedHeads":3,"installedStations":5,"rateUnitsPerHour":50,"hours":8}'
```

Fails with `400` if `plannedHeads > installedStations`.

### `POST /paths/{pathId}/work-units` — EnqueueWorkUnit

```sh
curl -X POST localhost:8080/paths/pick-a/work-units \
  -H 'Content-Type: application/json' \
  -d '{"workUnitId":"wu-1","cpt":"2026-08-21T12:00:00Z","reference":"order-line-1"}'
```

### `POST /paths/{pathId}/release` — ReleaseNextWork

Applies the release policy: admits the pending work unit with the earliest
(most urgent) CPT.

```sh
curl -X POST localhost:8080/paths/pick-a/release
```

Fails with `409` if the pool is empty or a release-fed pool is at its WIP
limit.

### `POST /work-units/{id}/complete` — RecordCompletion

```sh
curl -X POST localhost:8080/work-units/wu-1/complete
```

Fails with `409` if the unit was never released, or already completed.

### `GET /paths/{pathId}/telemetry` — SampleBacklog

```sh
curl localhost:8080/paths/pick-a/telemetry
```

Returns backlog depth, WIP, feed mode, and whether the path is over its
alarm threshold. Publishes `BacklogThresholdBreached` when over threshold.

### `GET /paths/{pathId}/rebalance` — RebalanceDecision

```sh
curl localhost:8080/paths/pick-a/rebalance
```

Recommends `ThrottleUpstream` (flow-fed path over its alarm threshold) or
`ReassignLabor` (release-fed path saturated at its WIP limit with backlog
remaining), or `NoActionNeeded`.

### `GET /healthz`

```sh
curl localhost:8080/healthz
```

## Testing

```sh
go build ./...
go vet ./...
go test ./...
```

Postgres repositories have a build-tagged integration test suite that is
skipped unless `DATABASE_URL` is set:

```sh
DATABASE_URL="postgres://wes:wes@localhost:5432/wes?sslmode=disable" \
  go test -tags=integration ./internal/adapters/outbound/postgres/...
```

## Invariants

Three aggregate invariants are enforced in the domain layer and each has a
failing-path unit test:

- **ShiftPlan/PathPlan**: `plannedHeads <= installedStations`
  (`internal/domain/plan/path_plan_test.go`).
- **WorkUnit**: assigned/released at most once, cannot complete twice, and
  cannot complete before release (`internal/domain/workunit/work_unit_test.go`).
- **WorkPool**: at-most-once handout per entry, and a release-fed pool's WIP
  limit is a hard invariant on release (`internal/domain/release/work_pool_test.go`).
