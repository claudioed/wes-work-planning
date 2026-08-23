---
id: index
title: WES — Work Planning & Release
sidebar_label: Introduction
sidebar_position: 0
slug: /overview/
description: The core bounded context of the Warehouse Execution System — the conductor that turns a shift's charge into a plan and releases work continuously.
---

# WES — Work Planning & Release

This is the **core bounded context of the WES (Warehouse Execution System) tier**
of the `warehouse-systems` platform. Its job in one sentence:

> Turn a shift's **charge** (the volume due by each CPT) into a **plan**
> (rate × heads per process path), then **release work continuously** in
> priority order and **flow-balance** the floor from live buffer telemetry.

It is the **conductor**: downstream of WMS planning and inventory truth,
upstream of WCS equipment control and of task execution. It never owns
inventory truth, never schedules a shift's workforce, and never drives a PLC —
it decides *when* work enters the floor and *in what order*.

## The three-layer position

| Layer | Horizon | Answers | This service |
|---|---|---|---|
| **WMS** | minutes → days | *What needs to happen, and why* | upstream of us |
| **WES** | seconds → minutes | *Who does it, right now, in what order* | **this service** |
| **WCS** | milliseconds → seconds | *How the machine performs the next step* | downstream of us |

## What you'll find here

| Section | Contents |
|---|---|
| [Overview](./what-it-does.md) | What the service does, its hexagonal layering, how to run it |
| [Business Context](../business-context/index.md) | Domain vision, ubiquitous language, the reasoning behind waveless release and Drum-Buffer-Rope flow balancing |
| [Domain-Driven Design](../ddd/index.md) | Subdomain classification, every aggregate and its invariants, every domain event, read models, context relationships |
| [API Reference](../api/index.md) | The REST API generated from the real `apis/openapi.yaml`, the event contract from `apis/asyncapi.yaml`, and the RFC 7807 error model |
| [Ecosystem](../ecosystem/index.md) | The context map, Kafka topics actually wired today, and the four sibling services |
| [ADRs](../adr/index.md) | Seven architecture decision records reconstructed from this repo's real history |

## At a glance

- **Language / runtime**: Go 1.26, module `github.com/claudioed/wes-work-planning`
- **Architecture**: hexagonal (ports & adapters), enforced by executable
  fitness tests ([ADR-0007](../adr/0007-arch-go-fitness-tests.md))
- **Inbound adapters**: REST (chi) and Kafka consumer
- **Outbound adapters**: Postgres (pgx/v5), in-memory, log event publisher,
  Kafka event publisher
- **Aggregates**: `ChargeForecast`, `ShiftPlan`/`PathPlan`, `WorkPool`, `WorkUnit`
- **Read models (projections, not aggregates)**: `LaborPlanObserved`,
  `UsableInventoryObserved`, backlog telemetry, rebalance recommendation
- **Integration**: publishes `WorkReleased`; consumes `ShiftPlanCommitted`,
  `StockReserved`, `ReservationRevoked`, `TaskCompleted`
