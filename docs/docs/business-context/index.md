---
id: index
title: Business Context
sidebar_label: Introduction
sidebar_position: 0
slug: /business-context/
description: Why this bounded context exists, what problem it solves, and the language it speaks.
---

# Business Context

A distribution centre has one hard, recurring problem: **a fixed amount of
volume must physically clear the building before a set of truck departures.**
Missing a departure is not a delay — it is a missed delivery promise for every
parcel that should have been on that trailer.

Two things make that problem non-trivial:

1. **The deadline is not a single number.** Different parcels have different
   trucks, so the building faces a *staircase* of deadlines through the shift,
   not one end-of-day target. That staircase is the **charge**.
2. **Capacity is not a single number either.** The floor is a set of *process
   paths*, each a queue with its own service rate and its own staffed
   capacity. Volume clears only as fast as the slowest path that touches it.

This bounded context exists to reconcile those two staircases in real time. It
is the only place in the platform that holds both at once.

## In this section

| Page | What it covers |
|---|---|
| [Domain vision](./domain-vision.md) | The vision statement, the problem, and the boundary this context refuses to cross |
| [Ubiquitous language](./ubiquitous-language.md) | Every term with its real definition, including the traps |
| [Why waveless release](./why-waveless-release.md) | The reasoning behind continuous release instead of wave batching |
| [Flow balancing](./flow-balancing.md) | Drum-Buffer-Rope with CPT as the drum, and the two corrective levers |
