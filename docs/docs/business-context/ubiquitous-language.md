---
id: ubiquitous-language
title: Ubiquitous language
sidebar_label: Ubiquitous language
sidebar_position: 2
description: Every term used by the Work Planning & Release bounded context, with its exact definition and the traps to avoid.
---

# Ubiquitous language

These are the exact terms used in the code, the API, the events and the
conversation. Where a term is easy to get wrong, the wrong reading is stated
explicitly — a glossary that only lists right answers does not prevent the
mistakes people actually make.

## Core terms

| Term | Definition | **Not** |
|---|---|---|
| **Charge** | The volume that must clear a process path, bucketed by CPT. A set of `(CPT, quantity)` pairs. | Not "total due today". Flattening the buckets destroys the only signal that tells you whether you are on track. |
| **CPT** — Critical Pull Time | The last moment a parcel can be manifested and still make its truck. A value object on work; **priority derives from it**. | Not a due date, not an SLA, not a soft target. It is a physical departure constraint. |
| **Process Path** | A named station that owns a **queue**: unit-in → unit-out, direct or indirect, with a service rate and a staffed capacity. | Not a workflow step or a stage in a pipeline. |
| **Work Pool** | The queue for exactly one process path: backlog depth, arrival rate, service rate. | Not a global task list. One pool per path, always. |
| **WorkUnit** | A releasable unit of work (e.g. a pick task). Carries a CPT, is assigned at most once at a time, and cannot complete twice. | Not the downstream `Task` in `fulfillment-execution` — see the traps below. |
| **ShiftPlan / PathPlan** | This service's committed split of headcount across paths: rate × heads × hours per path. | Not `workforce-management`'s `ShiftPlan` — see the traps below. |
| **Release** | Continuous, priority-ordered admission of work into a pool. Waveless. | Not a schedule, not a batch job. The release decision is a **policy object**. |
| **Flow balancing** | On telemetry (backlog vs plan): throttle upstream release, or flag labour reassignment. Drum-Buffer-Rope with CPT as the drum. | Not "rebalance the warehouse". It is a bounded, two-lever correction on one path. |

## Feed modes

A work pool is one of two kinds, and the distinction changes which invariants
are enforceable:

| Feed mode | Meaning | WIP limit is… |
|---|---|---|
| **Release-fed** | WES controls the volume entering this pool. | …an **enforceable invariant**. `ReleaseNext` refuses past the limit. |
| **Flow-fed** | Work arrives by physical conveyance; WES controls priority only. | …only an **alarm threshold**. You cannot refuse a tote that a conveyor already delivered. |

This is not a configuration nicety. You can only *enforce* a limit on an input
you actually control; pretending otherwise produces an invariant that the
physical world violates several times a minute.

## Supporting terms

| Term | Definition |
|---|---|
| **Backlog depth** | Count of *pending* (not yet released) entries in a pool. A projection, not stored state. |
| **WIP** | Count of *released* entries in a pool that have not yet completed. |
| **Alarm threshold** | The backlog depth above which a flow-fed pool raises `BacklogThresholdBreached`. |
| **WIP limit** | The maximum outstanding released work on a release-fed pool. Enforced at release time. |
| **Installed stations** | The physical station count on a path. The ceiling for `plannedHeads`. |
| **Planned throughput** | `rate × plannedHeads × hours` for one path plan. |
| **Rate** | A service rate in units per hour. Must be positive. |
| **Reference** | The external identifier a work unit points back at (e.g. an order line). Required and non-empty. |
| **LaborPlanObserved** | Read-only projection, keyed by `path_id`, of the labour plan Workforce Management last committed. |
| **UsableInventoryObserved** | Read-only projection, keyed by **SKU**, of usable quantity as Inventory last reported it. |
| **SKU** (on `WorkUnit`) | Optional inventory SKU the work unit's order line corresponds to. Empty is valid — not every caller knows one. Exists solely so `ReleaseNextWork`'s outbound publisher can look up a `ProductClassificationView` once, at release time. |
| **ProductClassificationView** | A plain, un-persisted read model — the result of a **synchronous** HTTP read from inventory-storage's `GET /products/{sku}/classification`, made once at release time. Not a Kafka projection (see the trap below). |
| **GiftWrap** (on `WorkUnit`) | Optional, caller-stated at enqueue time: whether the requester asked the warehouse to produce a gift package for this work unit. **Not** a product attribute or `ProductClassification` tag — the same SKU may be gift-wrapped on one order and not another. Stamped directly onto `WorkReleased` as `gift_wrap`, read straight off the `WorkUnit`, never looked up from `inventory-storage`. See [ADR-0010](../adr/0010-gift-wrap-as-a-work-released-characteristic.md). |

## The traps

DDD's classic hazard is *same word, different model*. This context sits at the
intersection of three others, and hits it twice.

### Trap 1 — `ShiftPlan` means two different things

`workforce-management` has a `ShiftPlan`. This service has a `ShiftPlan`. They
are **not** the same model and are deliberately not shared.

- **Ours** is a committed decision this context makes and enforces
  (`plannedHeads ≤ installedStations`). It is an aggregate with invariants.
- **Theirs** is a fact about labour that arrives over Kafka as
  `ShiftPlanCommitted`. It is projected into a *different type* named
  `LaborPlanObserved` — a plain read-model value with no invariants at all.

Feeding Workforce's event into our aggregate would make an external system able
to violate our invariant, and would silently couple our commit logic to their
release cadence. See [ADR-0006](../adr/0006-labor-plan-view-not-shift-plan.md).

### Trap 2 — `WorkUnit` here is not `Task` downstream

The reference model calls this out directly: a business-level demand signal and
an execution-level unit bound to a worker at a moment are two different classes
with two different lifecycles, and sharing the class across the boundary is the
mistake.

Here, a `WorkUnit` is *releasable volume with a deadline*. In
`fulfillment-execution`, a `Task` is *claimable work with a lease*. The
`WorkReleased` integration event is the translation point — an anti-corruption
boundary, not a shared type.

### Trap 3 — read models are not aggregate state

Backlog depth, actual rate and plan-vs-actual are **projections**. They are
computed from pool state or built from events; none of them is a field
maintained on an aggregate. Storing them on an aggregate would create a second
source of truth that drifts. See [Read models](../ddd/read-models.md).

### Trap 4 — `ProductClassificationView` is not `UsableInventoryObserved`

Both originate in `inventory-storage`, both are keyed by SKU, and it would be
easy to assume they arrive the same way. They do not.

`UsableInventoryObserved` is a **persisted Kafka projection**: `StockReserved`
and `ReservationRevoked` are part of inventory-storage's published integration
contract, so this service consumes them continuously and keeps a running
read model in Postgres/memory.

`ProductClassificationView` is a **synchronous HTTP read, not persisted at
all**. inventory-storage's `ProductClassified` event exists in its domain-event
catalogue but its own outbound Kafka publisher explicitly does not forward it
to the broker (see that repo's `publisher.go` doc comment) — there is nothing
to consume. Instead, `ReleaseNextWork`'s outbound publisher calls
`GET /products/{sku}/classification` **once**, at the moment a unit is
released, and stamps the result onto that one `WorkReleased` event. See
[ADR-0009](../adr/0009-product-classification-propagation-to-work-released.md).

### Trap 5 — `GiftWrap` is not a `ProductClassification` tag

Both `gift_wrap` and `fragile` are optional booleans on the same
`WorkReleased.data` payload, both omitted when false — it would be easy to
assume they arrive the same way. They do not.

`fragile` is **derived**: looked up from `inventory-storage`'s
`ProductClassification` for the released unit's SKU, via the synchronous
`ProductClassificationLookup` port (see Trap 4 above and
[ADR-0009](../adr/0009-product-classification-propagation-to-work-released.md)).
It says something about the *product*.

`gift_wrap` is **caller-supplied**: stated on `EnqueueWorkUnitRequest` at
enqueue time and read straight off the `WorkUnit`, exactly like `cpt` or
`reference`. It never touches `inventory-storage`, has no fail-open lookup
concern, and says something about *this particular unit of work* — the same
SKU can be gift-wrapped on one order and not on another. See
[ADR-0010](../adr/0010-gift-wrap-as-a-work-released-characteristic.md).
