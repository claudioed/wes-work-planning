---
id: integration-events
title: Integration events
sidebar_label: Integration events
sidebar_position: 2
description: Every Kafka topic this service publishes to and consumes from, with real payloads, configuration and idempotency behaviour.
---

# Integration events

All five services share **one Kafka broker** (`localhost:9092` locally, via
`~/warehouse-systems/docker-compose.kafka.yml`). This service does not run its
own — it connects to the shared one.

Client library: `github.com/segmentio/kafka-go` (pure Go, no cgo).

## The shared envelope

Every service exchanges the same outer shape:

```json
{
  "event_id": "uuid-v4",
  "event_type": "WorkReleased",
  "occurred_at": "2026-08-21T22:00:00Z",
  "source": "wes-work-planning",
  "data": { }
}
```

`event_id` is a UUID v4 generated at publish time and is also the Kafka
**message key**. `source` is always the publishing service's own name. `data`
is event-type-specific.

The envelope struct is defined in `internal/adapters/kafka/envelope` and is
**duplicated by agreement** in each service rather than extracted into a shared
library — so no service can force another to redeploy by changing a shared
dependency version.

:::note Envelope vs. the AsyncAPI spec
`apis/asyncapi.yaml` documents a CloudEvents 1.0 envelope, which is the
platform's published target contract. The running adapters use the simpler
shape above. Both are described on the [Events](../api/events.md#the-cloudevents-envelope)
page; code against the shape above if you are writing a consumer today.
:::

## Published

### Topic `warehouse.work-planning.events`

| `event_type` | `data` | Published when | Consumed by |
|---|---|---|---|
| `WorkReleased` | `{"path_id","work_unit_id","cpt","ref"}` | `ReleaseNextWork` releases a unit | **`fulfillment-execution`** → creates a `Task` |

```json
{
  "event_id": "1d7e4b90-3c58-4d22-9a6f-8b1c0e5d7a23",
  "event_type": "WorkReleased",
  "occurred_at": "2026-08-21T22:12:30Z",
  "source": "wes-work-planning",
  "data": {
    "path_id": "pick-to-tote",
    "work_unit_id": "wu-10231",
    "cpt": "2026-08-22T02:00:00Z",
    "ref": "order-88421-line-3"
  }
}
```

The other eight domain events are also written to this topic by the outbound
adapter with a `{"path_id": ...}`-shaped payload, but nothing consumes them
today. See the [full catalogue](../api/events.md#events-published).

Set `EVENT_PUBLISHER=kafka` (with `KAFKA_BROKERS`) to publish here; the default
`log` publisher writes the same events to the log instead. Both implement the
same `ports.EventPublisher` interface, so the use cases cannot tell which is
wired.

## Consumed

Setting `KAFKA_BROKERS` starts the consumer automatically, **independent of
`EVENT_PUBLISHER`**. It reads four topics concurrently, one goroutine each.

### `warehouse.workforce.events` — `ShiftPlanCommitted`

```json
{
  "event_id": "...", "event_type": "ShiftPlanCommitted",
  "occurred_at": "...", "source": "workforce-management",
  "data": {
    "building_id": "BLD1", "shift_id": "S1", "path_id": "pick-a",
    "planned_heads": 7, "planned_rate": 95.5, "planned_hours": 8
  }
}
```

Projected into `LaborPlanObserved`, keyed by `path_id`, read at
`GET /paths/{pathId}/labor-plan-view`. Workforce publishes **one message per
path line** of its own shift plan, which is why the projection keys on
`path_id` with one row per path.

**It is not fed into this service's `ShiftPlan` aggregate or `CommitShiftPlan`
use case.** Same word, different bounded context —
[ADR-0006](../adr/0006-labor-plan-view-not-shift-plan.md).

### `warehouse.inventory.events` — `StockReserved`, `ReservationRevoked`

```json
{
  "event_id": "...", "event_type": "StockReserved",
  "occurred_at": "...", "source": "inventory-storage",
  "data": {"sku": "SKU-8891", "quantity": 4, "demand_ref": "order-88421"}
}
```

Both types carry the same `data` shape. `StockReserved` **decrements** the
observed usable count for that SKU; `ReservationRevoked` **increments** it
back. Projected into `UsableInventoryObserved`, keyed by **SKU**, read at
`GET /inventory-view/{sku}`.

Keyed by SKU and not by path deliberately: Inventory reservations are
SKU-scoped, and a SKU-to-path mapping does not exist in the domain.

### `warehouse.fulfillment.events` — `TaskCompleted`

```json
{
  "event_id": "...", "event_type": "TaskCompleted",
  "occurred_at": "...", "source": "fulfillment-execution",
  "data": {"task_id": "t-551", "station_id": "pack-3", "work_unit_id": "wu-10231"}
}
```

`data.work_unit_id` maps to `RecordCompletionRequest.WorkUnitId` and calls the
**existing** `RecordCompletion` use case — the exact code path
`POST /work-units/{id}/complete` uses. No new use case; the inbound adapter is
the only new thing.

### `warehouse.order-management.events` — `OrderAllocated`, `OrderPartiallyAllocated`

```json
{
  "event_id": "...", "event_type": "OrderAllocated",
  "occurred_at": "...", "source": "order-management",
  "data": {
    "order_id": "order-1", "promise_date": "2026-08-22T02:00:00Z",
    "lines": [{"line_no": 1, "sku": "SKU-1", "path_id": "pick-a", "gift_wrap": false}]
  }
}
```

Both event types share this identical `data` shape and are handled
identically — both mean "these lines are ready to enqueue". This is the
event-choreography replacement for order-management's former **synchronous**
call to `POST /paths/{pathId}/work-units`: order-management (a new, 6th
bounded context, upstream Customer of this service) now publishes here once
it has allocated stock and locally marked an order line Released.

For each entry in `lines`, the handler calls the **existing** `EnqueueWorkUnit`
use case directly (no new use case), deriving a **deterministic**
`work_unit_id` as `"{order_id}-line-{line_no}"` — so the same order line
always maps to the same work unit, which is a second line of defense against
duplicate enqueues on top of the `processed_events` idempotency guard (see
below).

This integration is deliberately **fire-and-forget**: there is no reply event
back to order-management. The existing `WorkUnitCreated`/`WorkReleased`
events already published on `warehouse.work-planning.events` remain the only
observable signal of downstream progress — the same signal every other
`EnqueueWorkUnit` caller (including the REST endpoint) already relies on.
This was a confirmed v1 design choice made when rejecting order-management's
former synchronous HTTP coupling, not an oversight.

## Idempotency

Kafka is at-least-once, so redelivery is normal, not exceptional. Every
consumer path here is idempotent by construction:

```mermaid
flowchart LR
    M["message arrives"] --> P{"insert event_id into<br/>processed_events"}
    P -->|"primary-key collision<br/>(already processed)"| SKIP["skip the effect<br/><b>ack anyway</b>"]
    P -->|"inserted"| APPLY["apply the effect<br/>(projection or use case)"]
    APPLY --> ACK["ack"]
```

- **Postgres**: table `processed_events (event_id TEXT PRIMARY KEY, processed_at
  TIMESTAMPTZ)`, added by its own migration. The primary-key violation *is* the
  duplicate check — no read-then-write race.
- **In-memory**: a mutex-guarded `map[string]struct{}` with the same semantics.

Both sit behind one port, `ProcessedEventRepo.TryMarkProcessed(ctx, eventId,
at) (alreadyProcessed bool, err error)`.

Observable consequences, each covered by a unit test:

| Redelivered event | Effect |
|---|---|
| `StockReserved` | usable quantity is **not** double-decremented |
| `ReservationRevoked` | usable quantity is **not** double-incremented |
| `ShiftPlanCommitted` | the labour projection is **not** re-written |
| `TaskCompleted` | `RecordCompletion` is **not** called a second time |
| `OrderAllocated` / `OrderPartiallyAllocated` | `EnqueueWorkUnit` is **not** called a second time per line |

The last one matters operationally: `WorkUnit.Complete` already rejects
double-completion with `ErrAlreadyCompleted`, so the aggregate would be safe
regardless. But without the `event_id` check, every redelivery would surface a
domain error from a perfectly normal Kafka behaviour, and an error that is
sometimes meaningless is an error nobody reads. Deduplicating first keeps
`ErrAlreadyCompleted` meaning what it says.

## Configuration

| Env var | Default | Effect |
|---|---|---|
| `KAFKA_BROKERS` | *(unset)* | Comma-separated brokers. **Setting it starts the inbound consumer.** |
| `EVENT_PUBLISHER` | `log` | `kafka` switches the outbound publisher; requires `KAFKA_BROKERS`. |

## Verifying it end to end

There is a build-tagged integration test (`//go:build integration`) that
publishes real `ShiftPlanCommitted`- and `StockReserved`-shaped messages to the
broker and asserts the read models update. It is skipped when `KAFKA_BROKERS`
is unset, so the default `go test ./...` never needs a broker:

```sh
KAFKA_BROKERS=localhost:9092 go test -tags=integration ./...
```

For a manual smoke test, see [Running locally](../overview/running-locally.md#connecting-to-the-shared-kafka-broker).
