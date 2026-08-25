---
id: index
title: Ecosystem
sidebar_label: Introduction
sidebar_position: 0
slug: /ecosystem/
description: Where this service sits among the six warehouse-systems bounded contexts.
---

# Ecosystem

`warehouse-systems` is six Go services, each a bounded context with its own
model, its own database and its own deployment lifecycle. This one is the WES
tier's core — the conductor.

| Page | Contents |
|---|---|
| [Context map](./context-map.md) | The diagram: what is actually wired today, and what is only strategically related |
| [Integration events](./integration-events.md) | Every topic published and consumed, with real payloads and idempotency behaviour |
| [Sibling services](./sibling-services.md) | What each of the other five owns |

## The six services

```mermaid
flowchart TB
    subgraph wms["WMS tier — what &amp; where"]
        INV["inventory-storage<br/><i>Core</i>"]
        OM["order-management<br/><i>Core</i>"]
    end
    subgraph wes["WES tier — when &amp; in what order"]
        WP["<b>wes-work-planning</b><br/><i>Core — this service</i>"]
        FE["fulfillment-execution<br/><i>Core</i>"]
    end
    subgraph sup["Supporting &amp; Generic"]
        WM["workforce-management<br/><i>Supporting</i>"]
        FL["facility-layout<br/><i>Generic</i>"]
    end

    INV -->|"StockReserved<br/>ReservationRevoked"| WP
    WM -->|"ShiftPlanCommitted"| WP
    OM -->|"OrderAllocated<br/>OrderPartiallyAllocated"| WP
    WP -->|"WorkReleased"| FE
    FE -->|"TaskCompleted"| WP

    style WP fill:#2e6da4,color:#ffffff,stroke:#1b4368,stroke-width:3px
    style FL stroke-dasharray: 5 5
```

Five live Kafka edges, all of them touching this service. `facility-layout`
(dashed) has no live integration with anything yet.
