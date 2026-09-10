# Integration & REST reference — wes-work-planning

## REST API (inbound adapter, 11 operationIds in apis/openapi.yaml)

- `GET  /healthz`                          → healthCheck
- `POST /paths/{pathId}/charge`            → receiveChargeForecast
- `POST /paths/{pathId}/plan`              → commitShiftPlan
- `POST /paths/{pathId}/work-units`        → enqueueWorkUnit
- `POST /paths/{pathId}/release`           → releaseNextWork
- `GET  /paths/{pathId}/telemetry`         → sampleBacklog
- `GET  /paths/{pathId}/rebalance`         → rebalanceDecision
- `GET  /paths/{pathId}/labor-plan-view`   → getLaborPlanView
- `GET  /work-units?reference=`            → getWorkUnitsByReference
- `POST /work-units/{id}/complete`         → recordCompletion
- `GET  /inventory-view/{sku}`             → getInventoryView

`GET /work-units?reference=` is the read side backing the fleet's
cross-service Order Lifecycle console screen — see ADR-0002 in
`warehouse-ops-agent`'s docs and this repo's own adoption-record ADR under
`docs/docs/adr/`. It returns every WorkUnit ever enqueued against a
caller-supplied `reference` (order-management's `OrderId`), array-shaped,
side-effect-free.

Full request/response schemas, every status code, and the shared `Problem`
error component: [`apis/openapi.yaml`](../../apis/openapi.yaml). The
Docusaurus REST reference (`docs/docs/api/rest/*.api.mdx`, 11 files, one per
operationId + tag pages) is **generated** from this spec by
`docusaurus-plugin-openapi-docs` — regenerate with
`cd docs && npm run gen-api-docs` (or let the `prebuild` script do it as
part of `npm run build`); never hand-edit the generated `.api.mdx`/`.json`
files.

## Kafka integration events

Client library: `github.com/segmentio/kafka-go`. Shared envelope (identical
across the fleet's services, though `apis/asyncapi.yaml` documents the
richer CloudEvents 1.0 shape as the target contract — see
`docs/docs/api/events.md`'s explicit note reconciling the two):

```json
{
  "event_id": "uuid-v4",
  "event_type": "WorkReleased",
  "occurred_at": "2026-08-21T22:00:00Z",
  "source": "wes-work-planning",
  "data": { }
}
```

### Published

Topic `warehouse.work-planning.events`:

- `WorkReleased` — published when `ReleaseNextWork` releases a unit.
  `data`: `{"path_id","work_unit_id","cpt","ref"}` (+ optional
  `required_capabilities`/`fragile` from product-classification
  propagation). Consumed downstream by `fulfillment-execution` → creates a
  `Task`.
- All other domain events are also published to this topic for
  observability; only `WorkReleased` has a live consumer today.

Topic `warehouse.wes.analytics` (separate, additive — see
`.claude/rules/architecture.md`'s Analytics section): every domain event,
fanned out alongside the integration topic when `EVENT_PUBLISHER=kafka`,
consumed only by `cmd/wes-projector`.

### Consumed

`internal/adapters/inbound/kafka/consumer.go` subscribes to:

1. `warehouse.workforce.events`, event_type `ShiftPlanCommitted` →
   projected into the read-only `LaborPlanObserved` (`internal/domain/laborview/`),
   keyed by `path_id`. **Not** fed into this service's own
   `ShiftPlan`/`PathPlan` aggregate (ADR-0006 — same term, different bounded
   context).
2. `warehouse.inventory.events`, event_types `StockReserved` /
   `ReservationRevoked` → projected into `UsableInventoryObserved`
   (`internal/domain/inventoryview/`), keyed by SKU.
3. `warehouse.fulfillment.events`, event_type `TaskCompleted` → calls the
   existing `RecordCompletion` use case directly (closes the
   Execution → Orchestration feedback loop).
4. `warehouse.order-management.events`, event_types `OrderAllocated` /
   `OrderPartiallyAllocated` (identical payload shape) → fed directly into
   the existing `EnqueueWorkUnit` use case, one call per allocated line.
   This **replaces** order-management's former synchronous HTTP call to
   `POST /paths/{pathId}/work-units` with event choreography — verify
   against `internal/adapters/inbound/kafka/consumer.go`'s doc comment
   before assuming scope, this list grows.

### Idempotency

Every consumer path is idempotent under Kafka's at-least-once delivery via a
`processed_events (event_id TEXT PRIMARY KEY, processed_at TIMESTAMPTZ)`
table (Postgres) or a thread-safe map (in-memory adapter): insert the
`event_id` before applying an effect; skip if already present.
`RecordCompletion` additionally rejects double-complete at the domain level
as defense in depth.

### Transactional outbox (ADR-0014)

With `EVENT_PUBLISHER=kafka` **and** `DATABASE_URL` set, events are written
to the `outbox_events` table in the same transaction as the aggregate change
and relayed to both Kafka topics by an in-process relay
(`internal/adapters/outbound/postgres/` UnitOfWork +
`internal/adapters/outbound/kafka/` RelaySink) — not published directly
in-request. Without `DATABASE_URL`, events publish directly (no outbox).

## AsyncAPI narrative staleness — checked 2026-09 (report only, no drift found)

`apis/asyncapi.yaml` declares 9 messages (`ChargeForecastReceived`,
`ShiftPlanCommitted`, `WorkUnitCreated`, `WorkReleased`, `WorkUnitCompleted`,
`BacklogThresholdBreached`, `RateDeviationDetected`, `PathThrottled`,
`LaborReassignmentFlagged`) on channel `warehouse.work-planning.events`, and
narrative docs (`docs/docs/ddd/domain-events.md`,
`docs/docs/api/events.md`, `docs/docs/ecosystem/integration-events.md`)
document all 9 accurately, including the honest caveat that
`RateDeviationDetected` is declared but not yet raised by any use case, and
that the AsyncAPI CloudEvents envelope is the target contract while the
running adapters use the simpler flat envelope above. This fleet documents
AsyncAPI narratively per-service by design; the generated AsyncAPI static
site only exists in the separate fleet-wide docs aggregator repo, not here.
No stale narrative content found — re-check this note if messages or
channels change in `apis/asyncapi.yaml` without a matching narrative update.
