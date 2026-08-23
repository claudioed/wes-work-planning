# WES — Work Planning & Release

> **⚠️ Study project.** This repository is an educational exercise in
> Domain-Driven Design applied to warehouse management/execution systems. It
> follows real industry-standard patterns and terminology (WMS/WES/WCS,
> waveless release, CloudEvents, RFC 7807, hexagonal architecture) but is
> **not a production system** and is **not affiliated with, endorsed by, or
> representative of Amazon or any other company**.

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

| Env var          | Default | Purpose                                                                 |
|------------------|---------|--------------------------------------------------------------------------|
| `HTTP_ADDR`      | `:8080` | Address the HTTP server listens on                                     |
| `DATABASE_URL`   | (unset) | Postgres DSN; falls back to in-memory if unset                         |
| `EVENT_PUBLISHER`| `log`   | `log` (default) or `kafka` — where domain events get published         |
| `KAFKA_BROKERS`  | (unset) | Comma-separated Kafka brokers; required for `EVENT_PUBLISHER=kafka` and enables the inbound integration-event consumer whenever set |

## API

All bodies/responses are JSON. Timestamps are RFC3339. Every endpoint is also
documented exhaustively (full request/response schemas, every status code, a
`Problem` component reused across every error response) in
[`apis/openapi.yaml`](./apis/openapi.yaml).

### Errors

Every error response (4xx/5xx) is [RFC 7807](https://www.rfc-editor.org/rfc/rfc7807)
`application/problem+json`, not a bespoke shape:

```sh
curl -sD - localhost:8080/paths/does-not-exist/telemetry
```

```
HTTP/1.1 404 Not Found
Content-Type: application/problem+json

{
  "type": "https://errors.wes-work-planning.warehouse-systems.dev/not-found",
  "title": "Resource not found",
  "status": 404,
  "detail": "resource not found",
  "instance": "/paths/does-not-exist/telemetry"
}
```

`type` is a stable per-category identifier (does not need to resolve to a
real page), `title` is a fixed category summary, `status` duplicates the HTTP
status code, `detail` is the specific error message for this occurrence, and
`instance` is the request path that produced it.

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

### `GET /paths/{pathId}/labor-plan-view` — LaborPlanView (read model)

```sh
curl localhost:8080/paths/pick-a/labor-plan-view
```

The latest labor plan Workforce Management reported for this path, projected
from its `ShiftPlanCommitted` integration event. See **Integration** below —
this is a separate read model from this service's own `ShiftPlan` aggregate.
`404` if nothing has been observed yet.

### `GET /inventory-view/{sku}` — InventoryView (read model)

```sh
curl localhost:8080/inventory-view/sku-1
```

The latest usable-quantity projection for a SKU, from Inventory's
`StockReserved`/`ReservationRevoked` integration events. `404` if nothing
has been observed yet.

### `GET /healthz`

```sh
curl localhost:8080/healthz
```

## Local development / quality gate

A `Makefile` mirrors the CI sensors so they can be run locally, before pushing.
`make help` lists every target.

```sh
make check        # fast pre-commit loop: fmt-check, vet, build, lint, test -race
make check-all    # check + 90% coverage gate + arch-test + bdd (pre-push gate)
make vuln         # govulncheck ./... — known CVEs in deps and the Go stdlib
make mutation     # fast blocking mutation subset CI enforces (./internal/domain/release)
```

`make integration` and `make mutation-all` are excluded from both bundles: the
first needs a running Postgres (`DATABASE_URL`), the second is the slow,
exhaustive mutation run that CI keeps on a weekly schedule.

Git hooks are managed with [lefthook](https://github.com/evilmartians/lefthook)
via `lefthook.yml` — `pre-commit` runs `make fmt-check`, `make vet` and
`make lint`; `pre-push` runs `make check`. Activate them once per clone:

```sh
brew install lefthook          # or: go install github.com/evilmartians/lefthook@latest
lefthook install
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

The Kafka consumer has a build-tagged integration test suite that is skipped
unless `KAFKA_BROKERS` is set (see **Integration** below):

```sh
KAFKA_BROKERS="localhost:9092" \
  go test -tags=integration ./internal/adapters/inbound/kafka/...
```

### BDD / Acceptance tests

Executable specifications live in `features/` as Gherkin `.feature` files and
are run with [godog](https://github.com/cucumber/godog), the official Cucumber
BDD framework for Go. The suite is black-box: it starts the real chi router
over the in-memory adapters in an `httptest` server and drives the REST API
with real HTTP calls (`features_test.go` at the repo root).

```sh
go test ./... -run TestFeatures -v
```

Scenarios cover committing a ShiftPlan against installed-station capacity,
CPT-priority Release with at-most-once handout, RecordCompletion plus the
backlog telemetry read model, and flow-balancing rebalance decisions. CI runs
them in the `bdd` job.

## Integration

This service both publishes and consumes integration events over Kafka
(`github.com/segmentio/kafka-go`), on the shared broker other warehouse-systems
services also use (`~/warehouse-systems/docker-compose.kafka.yml`,
`localhost:9092` by default). Every service uses the same envelope:

```json
{
  "event_id": "uuid-v4",
  "event_type": "WorkReleased",
  "occurred_at": "2026-08-21T22:00:00Z",
  "source": "wes-work-planning",
  "data": { }
}
```

### Publishes

Topic `warehouse.work-planning.events`:

- **`WorkReleased`** — published when `ReleaseNextWork` releases a unit.
  `data`: `{"path_id","work_unit_id","cpt","ref"}`. Consumed downstream by
  fulfillment-execution, which turns it into a Task.

Set `EVENT_PUBLISHER=kafka` (and `KAFKA_BROKERS`) to publish here instead of
the default `log` publisher; `internal/adapters/outbound/kafka` implements the
same `ports.EventPublisher` interface the log publisher does.

### Consumes

- Topic `warehouse.workforce.events`, event type `ShiftPlanCommitted` —
  `data`: `{"building_id","shift_id","path_id","planned_heads","planned_rate","planned_hours"}`.
  Projected into the `LaborPlanObserved` read model
  (`GET /paths/{pathId}/labor-plan-view`) — **not** fed into this service's own
  `ShiftPlan`/`CommitShiftPlan`, which is a different model in a different
  bounded context that happens to share the term "ShiftPlan".
- Topic `warehouse.inventory.events`, event types `StockReserved` and
  `ReservationRevoked` — `data` for both: `{"sku","quantity","demand_ref"}`.
  `StockReserved` decrements, `ReservationRevoked` increments, the
  `UsableInventoryObserved` read model, keyed by SKU
  (`GET /inventory-view/{sku}`).
- Topic `warehouse.fulfillment.events`, event type `TaskCompleted` — `data`:
  `{"task_id","station_id","work_unit_id"}`. Closes the control loop's
  feedback edge from fulfillment-execution back to this service:
  `data.work_unit_id` is fed directly into the existing `RecordCompletion` use
  case (`RecordCompletionRequest.WorkUnitId`), transitioning that work unit
  from Released to Completed exactly as `POST /work-units/{id}/complete`
  would. No new use case — this is additive wiring only.

Setting `KAFKA_BROKERS` starts this consumer automatically, independent of
`EVENT_PUBLISHER`.

### Idempotency

Kafka is at-least-once. Every consumed event's `event_id` is recorded in
`processed_events` (Postgres) / an in-memory set before its effect is
applied; a redelivered `event_id` is skipped (and still acked) rather than
re-applied. See `internal/application/usecases/observe_labor_plan.go` and
`observe_inventory_change.go`. `TaskCompleted` reuses this same mechanism from
the inbound Kafka adapter itself (`internal/adapters/inbound/kafka/consumer.go`)
since it calls `RecordCompletion` directly rather than through a projector use
case; `RecordCompletion`'s own domain-level double-complete rejection is a
second, independent safety net, not a substitute for it.

### Smoke-testing against the shared broker

With the shared broker running (`docker compose -f
~/warehouse-systems/docker-compose.kafka.yml up -d`) and this service running
with `KAFKA_BROKERS` set:

```sh
docker exec -i warehouse-kafka /opt/kafka/bin/kafka-console-producer.sh \
  --broker-list localhost:9092 --topic warehouse.workforce.events <<'EOF'
{"event_id":"evt-1","event_type":"ShiftPlanCommitted","occurred_at":"2026-08-21T20:00:00Z","source":"workforce-management","data":{"building_id":"bldg-1","shift_id":"shift-1","path_id":"pick-a","planned_heads":7,"planned_rate":95.5,"planned_hours":8}}
EOF

curl localhost:8080/paths/pick-a/labor-plan-view
# {"pathId":"pick-a","plannedHeads":7,"plannedRate":95.5,"plannedHours":8,"observedAt":"..."}
```

Same pattern for `warehouse.inventory.events` / `StockReserved` /
`GET /inventory-view/{sku}`.

For `TaskCompleted`, first get a work unit into Released state (enqueue then
release it), then publish the completion event and re-query the unit:

```sh
curl -X POST localhost:8080/paths/pick-a/work-units \
  -d '{"workUnitId":"wu-1","cpt":"2026-08-21T23:00:00Z","reference":"ref-1"}'
curl -X POST localhost:8080/paths/pick-a/release

docker exec -i warehouse-kafka /opt/kafka/bin/kafka-console-producer.sh \
  --broker-list localhost:9092 --topic warehouse.fulfillment.events <<'EOF'
{"event_id":"evt-task-1","event_type":"TaskCompleted","occurred_at":"2026-08-21T23:05:00Z","source":"fulfillment-execution","data":{"task_id":"task-1","station_id":"station-1","work_unit_id":"wu-1"}}
EOF

curl localhost:8080/paths/pick-a/telemetry
# work unit wu-1 is now Completed
```

## Observability

The service is instrumented with OpenTelemetry: traces and metrics are pushed
over **OTLP/gRPC** to a Collector, and logs are structured JSON on `log/slog`
carrying the active span's `trace_id`/`span_id`.

### Environment

| Variable | Default | Meaning |
| --- | --- | --- |
| `LOG_LEVEL` | `info` | `debug` \| `info` \| `warn` \| `error`, case-insensitive. JSON to stdout. |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `localhost:4317` | Collector's OTLP/gRPC receiver. Accepts the spec URL form (`http://host:4317`) or bare `host:port`. |
| `OTEL_SERVICE_NAME` | `wes-work-planning` | `service.name` resource attribute. |
| `SERVICE_VERSION` | `dev` | `service.version` resource attribute. |
| `ENVIRONMENT` | `local` | `deployment.environment.name` resource attribute. |

A Collector is *expected* at `OTEL_EXPORTER_OTLP_ENDPOINT` but never
*required*: the OTLP exporters dial lazily, so with nothing listening the
telemetry is silently dropped and the service starts and serves normally. In
the `warehouse-infra` kind cluster the Helm chart points this at the
in-cluster Collector Service (`charts/wes-work-planning/values.yaml`, the
`otel` block).

### What gets exported

**Traces**

- One server span per HTTP request (`github.com/riandyrn/otelchi`), named
  after the chi **route pattern** (`/paths/{pathId}/release`) rather than the
  raw path, so span names stay low-cardinality.
- A child span per database call (`github.com/exaring/otelpgx` as the pgx v5
  pool tracer), carrying the normalized SQL — parameter placeholders, never
  literal values.
- `kafka.publish <topic>` on the producer side and `kafka.consume <topic>` on
  the consumer side, per the OTel messaging semantic conventions. Trace
  context crosses the broker in the message's `traceparent` header
  (`internal/adapters/kafka/otelkafka`), so a `WorkReleased` published here
  and consumed by `fulfillment-execution` is one distributed trace — and a
  `ShiftPlanCommitted` published by `workforce-management` continues its
  trace into this service's projector.

**Metrics**

- `http.server.request.duration` (histogram, seconds) and
  `http.server.active_requests`, from otelchi's metric middleware.
- `wes.work_units.released` — the business metric: a counter incremented in
  the `ReleaseNextWork` use case (not in the HTTP handler, so it tracks the
  real domain event), attributed by `path_id`.
- Go runtime metrics — goroutines, GC, memory —
  (`go.opentelemetry.io/contrib/instrumentation/runtime`).

**Logs**

Every record is JSON on stdout. Records emitted while a span is active carry
that span's IDs, so logs join traces:

```json
{"time":"...","level":"INFO","msg":"http request","method":"POST","route":"/paths/{pathId}/release","status":200,"trace_id":"21da85e3a34ce1fabc425b63dfb148c6","span_id":"3af54929e6a0094c"}
```

The OTel SDK's own diagnostics are bridged onto the same logger at `debug`,
so nothing escapes as plain text.

### Smoke-testing trace propagation

With the shared broker running, publish a `WorkReleased` by releasing work,
then read the message back and compare its `traceparent` with the `trace_id`
of the request that produced it:

```sh
KAFKA_BROKERS=localhost:9092 EVENT_PUBLISHER=kafka LOG_LEVEL=info go run ./cmd/wes

curl -X POST localhost:8080/paths/pick-a/work-units \
  -H 'Content-Type: application/json' \
  -d '{"workUnitId":"wu-1","cpt":"2026-08-23T23:00:00Z","reference":"order-1"}'
curl -X POST localhost:8080/paths/pick-a/release

kafka-console-consumer.sh --bootstrap-server localhost:9092 \
  --topic warehouse.work-planning.events --property print.headers=true --from-beginning
```

The reverse direction works the same way: publish a `ShiftPlanCommitted` with
a `traceparent` header onto `warehouse.workforce.events` and the projector's
log line comes out carrying that same `trace_id`.

## Invariants

Three aggregate invariants are enforced in the domain layer and each has a
failing-path unit test:

- **ShiftPlan/PathPlan**: `plannedHeads <= installedStations`
  (`internal/domain/plan/path_plan_test.go`).
- **WorkUnit**: assigned/released at most once, cannot complete twice, and
  cannot complete before release (`internal/domain/workunit/work_unit_test.go`).
- **WorkPool**: at-most-once handout per entry, and a release-fed pool's WIP
  limit is a hard invariant on release (`internal/domain/release/work_pool_test.go`).

## Documentation

Full documentation site: **<https://claudioed.github.io/wes-work-planning/>**

It covers the business context and ubiquitous language, the DDD model
(subdomain classification, every aggregate and invariant, all nine domain
events), an API reference generated from `apis/openapi.yaml` plus an Events
page from `apis/asyncapi.yaml`, the ecosystem context map, and seven
architecture decision records. Source lives in [`docs/`](./docs) (Docusaurus);
it is built and deployed to GitHub Pages by
[`.github/workflows/docs.yml`](./.github/workflows/docs.yml).
