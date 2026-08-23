---
id: context-relationships
title: Context relationships
sidebar_label: Context relationships
sidebar_position: 5
description: The strategic context-mapping patterns that govern this bounded context's relationships with the other four services.
---

# Context relationships

The technical wiring — topics, payloads, what is actually running — is on the
[Ecosystem](../ecosystem/context-map.md) pages. This page is the **strategic**
view: which context-mapping pattern governs each relationship, and why.

The vocabulary is Evans/Vernon as used by the platform's strategic reference:
Customer/Supplier (C/S), Conformist (CF), Anti-Corruption Layer (ACL),
Open-Host Service (OHS), Published Language (PL), Partnership (P).

## Relationship table

| Counterpart | Direction | Pattern | Notes |
|---|---|---|---|
| `inventory-storage` (WMS tier) | upstream of us | **Customer/Supplier + OHS/PL**, with an **ACL on our side** | WMS is the authoritative supplier of stock reality. We consume its published events only; we never read or write its aggregates. |
| `workforce-management` | upstream of us | **Customer/Supplier**, with an **ACL on our side** | Supplies committed labour plans as `ShiftPlanCommitted`. Translated at the boundary into a different type. |
| `fulfillment-execution` | downstream of us | **Customer/Supplier**, we are the supplier; **OHS + Published Language** | We publish `WorkReleased`; Execution builds its own `Task` from it. It does not get access to our `WorkPool`. |
| `fulfillment-execution` (feedback edge) | upstream of us | **Customer/Supplier**, roles reversed | `TaskCompleted` flows back and closes the loop. Two directed relationships between the same pair, not one bidirectional one. |
| `facility-layout` | — | **no relationship today**; would be **Conformist to its OHS** | Not wired at all. Its location-code hierarchy is Published Language other contexts would conform to for physical-location truth. |

## WMS → WES: Customer/Supplier, ACL in both directions

The strategic reference is explicit:

> **WMS → WES**: Customer/Supplier, with WMS as the upstream customer of
> *demand* and WES as supplier of *fulfilment progress*. WMS defines an
> **Open Host Service (OHS)** with a **Published Language** that WES conforms
> to. WES does **not** get write access to WMS's Order/Inventory aggregates —
> only to the published events/API.

and:

> **WMS ↔ WES boundary is an Anti-Corruption Layer in both directions**: WMS
> never reaches into WES's `Assignment` aggregate to pick a worker; WES never
> reaches into WMS's `Order` aggregate to check inventory truth.

In this codebase that ACL is a concrete, locatable thing — the inbound Kafka
adapter, `internal/adapters/inbound/kafka/consumer.go`. It holds private structs
whose only job is to be the *external* shape:

```go
type inventoryEventData struct {
    SKU       string `json:"sku"`
    Quantity  int    `json:"quantity"`
    DemandRef string `json:"demand_ref"`
}
```

That struct is unexported and never crosses into the application layer. What
crosses is a translated call into a projector use case. Inventory's wire format
can change without a single domain file being touched — which is the entire
point of an ACL, and the reason it is worth the extra type.

## WES → Execution: we become the Open Host Service

Downstream, this context is the supplier. `WorkReleased` on
`warehouse.work-planning.events` is our Published Language: a stable,
documented contract (`apis/asyncapi.yaml`) that consumers conform to.

The payload is enriched at the adapter with `cpt` and `ref` precisely so the
contract is *self-sufficient* — a consumer can build its own `Task` without
calling back into us. An OHS that forces callbacks is not much of a host.

Crucially, `fulfillment-execution`'s `Task` is **its own model**, built from our
event. The reference model names this trap directly: a business-level demand
signal and an execution-level unit bound to a worker at a moment are different
classes with different lifecycles, and sharing the class across the boundary is
the mistake to avoid.

## The feedback edge is a separate relationship

`fulfillment-execution` is downstream of us for release and **upstream of us for
completion**. These are two directed Customer/Supplier relationships between the
same pair of contexts, not one "bidirectional" relationship.

Modelling it as two matters: the two edges have different contracts
(`warehouse.work-planning.events` vs `warehouse.fulfillment.events`), different
payloads, and different failure modes. On the feedback edge we are the customer
and conform to *their* published `TaskCompleted` shape.

## The relationship that does not exist

`facility-layout` is the newest service and has **no live integration with this
context at all** — no shared topic, no API call, no dependency in either
direction. It publishes only to an in-process log publisher today.

Strategically it is a **Generic subdomain** providing an Open Host Service for
physical-location truth (`Site → Zone → Aisle → LocationSlot`, with an
industry-standard `Site-Area-Zone-Aisle-Bay-Level-Position` code). If this
context ever needs to reason about *where* a process path physically is — for
travel-aware release or congestion-aware balancing — it would become a
**Conformist** to that Published Language rather than modelling geography
itself. That follows the reference model's own guidance to extract generic
logic once instead of duplicating it in every context.

Until then, the honest diagram has no edge there. See the
[context map](../ecosystem/context-map.md).

## What we are *not*

Two patterns that would be wrong to claim here:

- **Not a Shared Kernel** with any sibling. No types, schemas or database
  tables are shared. The envelope shape is *duplicated by agreement* in each
  service's own `envelope` package — deliberately copied rather than extracted
  into a shared library, so no service can be forced to redeploy by another
  service's release.
- **Not a Partnership.** The reference model uses Partnership for contexts that
  must evolve together (orchestration and labour assignment as two halves of one
  optimisation loop). This platform split those into separate services with an
  event contract between them, which is Customer/Supplier — a looser coupling,
  chosen deliberately, and worth being accurate about rather than claiming the
  tighter pattern.
