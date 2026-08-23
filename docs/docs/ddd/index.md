---
id: index
title: Domain-Driven Design
sidebar_label: Introduction
sidebar_position: 0
slug: /ddd/
description: Subdomain classification, aggregates and invariants, domain events, read models, and context relationships.
---

# Domain-Driven Design

This section is the tactical and strategic DDD record for the **Work Planning &
Release** bounded context. Everything here is derived from the two reference
documents that govern the platform's strategic design and from this repository's
own model — no aspirational modelling.

| Page | Contents |
|---|---|
| [Subdomain classification](./subdomain-classification.md) | Core / Supporting / Generic, with the justification |
| [Aggregates and invariants](./aggregates-and-invariants.md) | All four aggregates, every invariant, every failing path |
| [Domain events](./domain-events.md) | The nine past-tense events and who raises them |
| [Read models](./read-models.md) | Projections — and why they are never aggregate state |
| [Context relationships](./context-relationships.md) | Customer/Supplier, ACL, OHS, Conformist — the strategic patterns |

## One-page summary

```mermaid
classDiagram
    class ChargeForecast {
        <<Aggregate Root>>
        PathId pathId
        CPTBucket[] buckets
        +TotalQuantity() Quantity
        +QuantityForCPT(cpt) Quantity
        ~at least one bucket~
    }
    class ShiftPlan {
        <<Aggregate Root>>
        PathPlan[] pathPlans
        +TotalHours() float
        ~at least one path plan~
    }
    class PathPlan {
        PathId pathId
        StationCount plannedHeads
        StationCount installedStations
        Rate rate
        float hours
        +PlannedThroughput() float
        ~plannedHeads &lt;= installedStations~
    }
    class WorkPool {
        <<Aggregate Root>>
        PathId pathId
        FeedMode mode
        int wipLimit
        int alarmThreshold
        +ReleaseNext() string
        +BacklogDepth() int
        +WIP() int
        ~at-most-once handout~
        ~WIP limit on release-fed~
    }
    class WorkUnit {
        <<Aggregate Root>>
        string id
        PathId pathId
        CPT cpt
        string reference
        State state
        +Release(at)
        +Complete(at)
        ~Pending -> Released -> Completed~
        ~no double-complete~
    }
    class ReleasePolicy {
        <<Domain Service>>
        +Apply(pool) string
    }

    ShiftPlan "1" *-- "1..*" PathPlan
    ReleasePolicy ..> WorkPool : applies to
    WorkPool ..> WorkUnit : references by id
```

Note the association style between `WorkPool` and `WorkUnit`: the pool holds
**entry records keyed by work unit id**, not `WorkUnit` objects. They are two
separate aggregates with two separate consistency boundaries, referenced by
identity — a pool's release bookkeeping and a work unit's lifecycle are updated
in the same use case but are not one transaction-shaped object.
