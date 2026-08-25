---
id: 0010-gift-wrap-as-a-work-released-characteristic
title: ADR-0010 — Gift wrap as a caller-stated WorkReleased characteristic, not a product attribute
sidebar_label: 0010 · Gift wrap on WorkReleased
sidebar_position: 10
description: Why gift wrap is threaded through EnqueueWorkUnitRequest and WorkReleased exactly like SKU/CPT/reference, and explicitly not modeled as an inventory-storage ProductClassification tag.
---

# ADR-0010 — Gift wrap is a `WorkReleased` characteristic, not a product attribute

## Status

**Accepted.**

## Context

A request came in to "implement gift wrap." Before building anything, the
ambiguity in that request had to be resolved: is gift wrap a **per-SKU
product attribute** — the same shape as `inventory-storage`'s
`ProductClassification` (`Hazmat`, `Fragile`, `TemperatureSensitive`,
`Oversized`, `HighValue`, see
[ADR-0009](./0009-product-classification-propagation-to-work-released.md))
— or is it something a **caller states at the moment work is enqueued**,
independent of what SKU (if any) is involved?

The human clarification settled this explicitly: gift wrap is **not** a
property of the product. The same SKU might ship gift-wrapped for one order
and bare for another — it is a fact about *this particular unit of work*,
supplied by whoever is asking the warehouse to produce it, not a fact
`inventory-storage` could ever answer via
`GET /products/{sku}/classification`. That rules out extending
`ProductClassificationView`/`classificationHints` (ADR-0009's mechanism) —
that pipeline exists specifically to propagate SKU-level master data
`inventory-storage` owns, and gift wrap has no such owner.

What gift wrap *does* resemble is `CPT`, `reference`, and `SKU` on
`EnqueueWorkUnitRequest`: caller-supplied facts about a `WorkUnit`, threaded
onto the aggregate at enqueue time, and later stamped onto the outbound
`WorkReleased` event so a downstream consumer
(`fulfillment-execution`, in a sibling change) has what it needs without a
callback. This service already has that mechanism built and proven; gift
wrap only needed to ride the same rail.

## Decision

**We will add an optional `GiftWrap bool` to `EnqueueWorkUnitRequest`,
thread it onto `WorkUnit` via a plain setter (mirroring `SKU`), and stamp it
onto `WorkReleased`'s `data` payload as `gift_wrap` — read directly off the
`WorkUnit`, never derived through `classificationHints` or any
`inventory-storage` lookup.**

1. **`WorkUnit` carries an optional `giftWrap bool`.** `GiftWrap()` /
   `SetGiftWrap(bool)` mirror `SKU()`/`SetSKU()` exactly: a separate setter
   rather than a `NewWorkUnit` parameter, so every existing caller and test
   fixture keeps compiling. Default `false` — most work is not gift-wrapped.
2. **`EnqueueWorkUnitRequest` gains `GiftWrap bool`, default `false`.**
   `Execute` calls `unit.SetGiftWrap(req.GiftWrap)` right next to the
   existing `unit.SetSKU(req.SKU)` call.
3. **The outbound Kafka adapter reads `GiftWrap()` straight off the
   `WorkUnit`**, in the same `p.workUnits.FindById` block that already loads
   `cpt`/`ref`/`sku` — not inside `classificationHints`, which stays a pure
   `inventory-storage` classification concern. When `true`,
   `data["gift_wrap"] = true`; **omitted entirely when `false`**, the same
   discipline `fragile` already uses, so an event with no gift-wrap request
   looks identical to a pre-this-feature event.
4. **No `inventory-storage` involvement whatsoever.** No new
   `ProductClassification` tag, no change to `ports.ProductClassificationLookup`
   or `productclassificationview`. Gift wrap and product classification are
   two independent mechanisms that happen to both enrich `WorkReleased`.
5. **DTO and OpenAPI/AsyncAPI mirror the `sku` field's documentation style**,
   explicitly cross-referencing this ADR so a future reader does not
   conflate the two mechanisms.

## Consequences

### Easier

- **No cross-service coupling.** Unlike ADR-0009's SKU classification read,
  gift wrap never leaves this service's own `EnqueueWorkUnit` → `WorkUnit` →
  `WorkReleased` pipeline. There is no synchronous HTTP call, no fail-open
  concern, no availability dependency on `inventory-storage`.
- **Reuses a proven pattern exactly.** A developer who understands how `SKU`
  or `reference` flows from request to published event already understands
  how `gift_wrap` flows — no new mental model.
- **Per-work-unit, not per-SKU, matches reality.** The same SKU can be
  gift-wrapped on one order and not another; modeling this as a
  `WorkReleased` characteristic instead of a `ProductClassification` tag is
  the only shape that is actually correct.

### Harder

- **The caller must know and state gift wrap at enqueue time.** There is no
  mechanism (and none is planned) for gift wrap to be decided or corrected
  after a `WorkUnit` is enqueued — it is captured once, like `SKU`, and
  carried forward. A request to gift-wrap an already-enqueued or
  already-released unit is out of scope for this decision.
- **Two similarly-shaped but differently-sourced `WorkReleased` fields.**
  `gift_wrap` and `fragile` are both optional booleans on the same payload,
  omitted when false — but one is caller-supplied and one is derived from a
  cross-service lookup. A reader must not assume they share a data source;
  this ADR and the inline code comments exist specifically so that question
  keeps getting the same answer.
