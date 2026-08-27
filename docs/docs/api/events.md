---
id: events
title: Events
sidebar_label: Events (AsyncAPI)
sidebar_position: 2
description: The CloudEvents envelope, the type naming convention, and every event this service publishes and consumes.
---

# Events

The asynchronous contract lives in
[`apis/asyncapi.yaml`](https://github.com/claudioed/wes-work-planning/blob/main/apis/asyncapi.yaml)
(AsyncAPI 2.6.0), linted in CI by Spectral against `.spectral.asyncapi.yaml`.
This page is written from that spec.

## The channel

| | |
|---|---|
| **Topic** | `warehouse.work-planning.events` |
| **Protocol** | Kafka (`localhost:9092` locally; the broker is shared by all five services) |
| **Message key** | the event id |
| **Default content type** | `application/cloudevents+json` |
| **Delivery** | at-least-once — **consumers must deduplicate** |

## The CloudEvents envelope

The published contract is **CloudEvents 1.0 structured mode**: context
attributes and the event-specific `data` payload travel together in one JSON
body.

```json
{
  "specversion": "1.0",
  "id": "1d7e4b90-3c58-4d22-9a6f-8b1c0e5d7a23",
  "source": "/warehouse/wes-work-planning",
  "type": "com.warehouse.wes.work-planning.workunit.WorkReleased",
  "subject": "wu-10231",
  "time": "2026-08-21T22:12:30Z",
  "datacontenttype": "application/json",
  "data": {
    "path_id": "pick-to-tote",
    "work_unit_id": "wu-10231",
    "cpt": "2026-08-22T02:00:00Z",
    "ref": "order-88421-line-3"
  }
}
```

| Attribute | Rule |
|---|---|
| `specversion` | always `"1.0"` |
| `id` | UUID v4 generated at publish time; **with `source`, this is the de-duplication key** |
| `source` | always `/warehouse/wes-work-planning` on this channel |
| `type` | see the naming convention below |
| `subject` | the aggregate instance the event is about — a `path_id` for charge/plan/work-pool events, a `work_unit_id` for work-unit events |
| `time` | RFC 3339, taken from the **domain clock**, not from publish time |
| `datacontenttype` | always `application/json` |

Consumers should **ignore `type` values they do not recognise**: new event
types may be added to this channel without a major version bump.

### The `type` naming convention

Shared by every bounded context in the platform:

```
com.warehouse.<subdomain>.<bounded-context>.<entity>.<EventName>
```

All segments lowercase except the final PascalCase event name, which matches
the past-tense domain event name used in the code. For this service the
subdomain is `wes` and the bounded context is `work-planning`:

```
com.warehouse.wes.work-planning.charge.ChargeForecastReceived
com.warehouse.wes.work-planning.plan.ShiftPlanCommitted
com.warehouse.wes.work-planning.workunit.WorkReleased
com.warehouse.wes.work-planning.workpool.PathThrottled
```

The `<entity>` segment names the aggregate that raised the event: `charge`
(ChargeForecast), `plan` (ShiftPlan/PathPlan), `workpool` (WorkPool and the
flow-balancing decisions taken against it), `workunit` (WorkUnit).

:::caution The wire format in the running code differs from the published spec
The AsyncAPI document describes the CloudEvents 1.0 structured-mode envelope
above — that is the platform's **published contract**.

The Go outbound adapter
(`internal/adapters/outbound/kafka/publisher.go`, via
`internal/adapters/kafka/envelope`) currently writes the platform's **earlier,
simpler envelope**, which is what all four services actually exchange today:

```json
{
  "event_id": "uuid-v4",
  "event_type": "WorkReleased",
  "occurred_at": "2026-08-21T22:00:00Z",
  "source": "wes-work-planning",
  "data": { }
}
```

The two carry the same information under different attribute names
(`event_id`↔`id`, `event_type`↔`type`, `occurred_at`↔`time`, `source` bare vs
URI-reference). The migration of the running adapters to CloudEvents is not
done in this repository, and the sibling services' consumers still expect the
simpler shape — so if you are writing a consumer **today**, code against the
JSON block immediately above. This is documented rather than papered over.
:::

## Events published

Topic `warehouse.work-planning.events`. Nine event types are catalogued; the
`data` shape of each is below.

| Event | `data` fields | Raised when |
|---|---|---|
| `ChargeForecastReceived` | `path_id` | a charge forecast is recorded for a path |
| `ShiftPlanCommitted` | `path_id` | **this** context commits its own rate × heads × hours plan |
| `WorkUnitCreated` | `path_id`, `work_unit_id` | a work unit is enqueued into a pool |
| **`WorkReleased`** | `path_id`, `work_unit_id`, `cpt`, `ref` | the release policy admits the earliest-CPT unit |
| `WorkUnitCompleted` | `path_id`, `work_unit_id` | a released unit completes |
| `BacklogThresholdBreached` | `path_id` | backlog depth crosses the pool's alarm threshold |
| `RateDeviationDetected` | `path_id` | *declared in the catalogue; **no use case raises it today*** |
| `PathThrottled` | `path_id` | flow balancing decides to throttle upstream release |
| `LaborReassignmentFlagged` | `path_id` | flow balancing recommends moving headcount |

**`WorkReleased` is the only one any other service consumes today** —
`fulfillment-execution` turns it into a `Task`. Its payload is enriched at the
adapter with `cpt` and `ref` (read from the `WorkUnit` repository) so the
downstream consumer never has to call back; the domain event itself carries
only the two identifiers.

`WorkReleased.data` also carries two OPTIONAL fields, present only when
there is a hint to give: `required_capabilities` (array, containing
`"hazmat"` when the released unit's SKU is classified `Hazmat` in
inventory-storage) and `fragile` (bool, `true` when the SKU is classified
`Fragile`). See "Product classification propagation" below.

Publication is opt-in at runtime: with the default `EVENT_PUBLISHER=log` these
events are written to the log publisher instead of Kafka. Set
`EVENT_PUBLISHER=kafka` and `KAFKA_BROKERS` to publish.

## Events consumed

These belong to **other** bounded contexts and are documented in their
services' own specs; they are listed here for orientation. Consumption starts
automatically whenever `KAFKA_BROKERS` is set, independent of
`EVENT_PUBLISHER`.

### `warehouse.workforce.events` — from `workforce-management`

| `event_type` | `data` | Effect here |
|---|---|---|
| `ShiftPlanCommitted` | `building_id`, `shift_id`, `path_id`, `planned_heads`, `planned_rate`, `planned_hours` | Upserts the `LaborPlanObserved` projection for `path_id` |

Workforce publishes **one message per path line**, which is why the projection
keys cleanly on `path_id`. This is *not* fed into this context's own
`ShiftPlan` aggregate — see
[ADR-0006](../adr/0006-labor-plan-view-not-shift-plan.md).

### `warehouse.inventory.events` — from `inventory-storage`

| `event_type` | `data` | Effect here |
|---|---|---|
| `StockReserved` | `sku`, `quantity`, `demand_ref` | **Decrements** `UsableInventoryObserved` for that SKU |
| `ReservationRevoked` | `sku`, `quantity`, `demand_ref` | **Increments** it back |

Keyed by SKU, not by path — Inventory reservations are SKU-scoped.

### `warehouse.fulfillment.events` — from `fulfillment-execution`

| `event_type` | `data` | Effect here |
|---|---|---|
| `TaskCompleted` | `task_id`, `station_id`, `work_unit_id` | `data.work_unit_id` is passed to the existing `RecordCompletion` use case |

This closes the control loop's feedback edge. No new use case was introduced —
the inbound adapter calls exactly the same code path that
`POST /work-units/{id}/complete` does.

## Product classification propagation (Task 9, synchronous HTTP)

`inventory-storage` owns SKU-level `ProductClassification` master data
(`Hazmat`/`Fragile`/`TemperatureSensitive`/`Oversized`/`HighValue` tags),
exposed synchronously at `GET /products/{sku}/classification`. Downstream,
`fulfillment-execution`'s `Task` benefits from knowing hazmat-capability and
fragile handling **at claim time**, without a live per-task callback into
inventory-storage.

**Integration mechanism: synchronous outbound HTTP read, not a Kafka
projection.** This was a deliberate finding, not a default: inventory-storage
publishes `ProductClassified` in its domain-event catalogue, but its own
outbound Kafka publisher (`internal/adapters/outbound/kafka/publisher.go`
there) explicitly forwards only `StockReserved` and `ReservationRevoked` to
the broker — see that file's package doc comment and
`apis/asyncapi.yaml`'s "Full catalog vs. actually published" note.
`ProductClassified` is not part of the published integration contract, so
there is nothing to consume; building a Kafka projector against it would be
building a consumer for an event that never reaches the broker. See
[ADR-0009](../adr/0009-product-classification-propagation-to-work-released.md)
for the full reasoning.

- New outbound adapter package `internal/adapters/outbound/productclassification/`
  implementing a new port `ports.ProductClassificationLookup`
  (`GetClassification(ctx, sku) (productclassificationview.ProductClassificationView, error)`):
  a plain `net/http` client (`Client`, mirrors inventory-storage's own
  `facilitylayout.Client` HTTP-adapter pattern) calling
  `GET {INVENTORY_STORAGE_BASE_URL}/products/{sku}/classification`, and a
  `PermissiveLookup` no-op that always reports `Known=false`. Selected via
  `PRODUCT_CLASSIFICATION_MODE=http|permissive` (default `permissive`, so
  existing tests/CI/deployments are unaffected).
- New read model `internal/domain/productclassificationview/` —
  `ProductClassificationView{SKU, HandlingTags, TemperatureClass, Known}` — a
  plain value, not persisted, not an aggregate.
- `WorkUnit` gains an optional `SKU` field (`SetSKU`/`SKU()`), threaded
  through from `EnqueueWorkUnitRequest.SKU` (new optional field, empty by
  default so no existing caller breaks).
- `ReleaseNextWork`'s outbound Kafka publisher
  (`internal/adapters/outbound/kafka/publisher.go`) looks up the released
  unit's SKU classification **once**, at publish time, and stamps two new
  OPTIONAL `WorkReleased.data` fields: `required_capabilities` (array,
  appends `"hazmat"` when the SKU carries the `Hazmat` tag) and `fragile`
  (bool, `true` when the SKU carries the `Fragile` tag). Both fields are
  **omitted** — not defaulted to an explicit empty array / `false` — when
  the SKU is unclassified, has no SKU at all, or the lookup is unavailable
  (permissive mode or a lookup error): this is strictly additive and
  backward compatible with `fulfillment-execution`'s existing `WorkReleased`
  consumer, which the sibling repo's parallel PR extends to read these same
  optional fields, defaulting to `false`/empty when absent.
- **Fail-open, not fail-closed.** Unlike inventory-storage's own
  `StowStock` placement check (which blocks a stow when a Hazmat/
  TemperatureSensitive SKU's lookup is unavailable), a classification-lookup
  problem here must never block or delay releasing work — it can only omit
  an optional enrichment. Documented as a known gap in ADR-0009's
  Consequences, in the same spirit as inventory-storage's ADR-0003 "no
  expiry sweeper" gap.

## Idempotency

Kafka is at-least-once, so **every** consumer path here is idempotent. Before
applying an event's effect, its `event_id` is inserted into `processed_events`
(Postgres) or a thread-safe set (in-memory). A primary-key collision means
"already processed": the effect is skipped and the message is acked anyway.

Consequences worth knowing:

- Replaying a `StockReserved` does **not** double-decrement usable inventory.
- Replaying a `ShiftPlanCommitted` does **not** re-write the labour projection.
- Replaying a `TaskCompleted` does **not** call `RecordCompletion` twice — so a
  redelivery never surfaces the aggregate's `ErrAlreadyCompleted` as a
  spurious error or a retry loop. The aggregate would reject it anyway; that
  is defence in depth, not the only net.

## Smoke-testing by hand

```sh
# publish a workforce-shaped message onto the shared broker
echo '{"event_id":"11111111-1111-4111-8111-111111111111",
       "event_type":"ShiftPlanCommitted",
       "occurred_at":"2026-08-23T09:00:00Z",
       "source":"workforce-management",
       "data":{"building_id":"BLD1","shift_id":"S1","path_id":"pick-a",
               "planned_heads":7,"planned_rate":95.5,"planned_hours":8}}' \
| kafka-console-producer.sh --bootstrap-server localhost:9092 \
    --topic warehouse.workforce.events

curl -s localhost:8080/paths/pick-a/labor-plan-view

# watch what this service publishes
kafka-console-consumer.sh --bootstrap-server localhost:9092 \
  --topic warehouse.work-planning.events --from-beginning
```
