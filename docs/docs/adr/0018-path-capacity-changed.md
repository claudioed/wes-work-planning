---
id: 0018-path-capacity-changed
slug: /adr/0018-path-capacity-changed
title: 0018. Publish per-path, per-CPT remaining admission capacity as PathCapacityChanged
sidebar_label: 0018. PathCapacityChanged
description: ADR 0018 — why wes-work-planning (not order-management) owns publishing real path capacity, the ReleaseFed-vs-FlowFed "known" rule, and correlation-by-CutoffAt-timestamp instead of process-path-management's cptId.
---

# 0018. Publish per-path, per-CPT remaining admission capacity as `PathCapacityChanged`

## Status

Accepted — implemented in the same change that introduced this record.

## Context

order-management's own ADR 0014, "The delivery promise is a CPT window
derived from fulfillment capability" (Accepted, merged to order-management's
`develop`, PR #52), established that a customer's delivery promise should be
a discrete CPT window derived from **real path feasibility**, not a
configured lead time. Its Step A (already shipped) added a
`ports.PathCapacity` outbound port:

```go
type PathCapacity interface {
	Remaining(pathId shared.PathId, cptId string) (units int, known bool)
}
```

with exactly one adapter today, `UnknownPathCapacity`, which always returns
`known=false` — a deliberate placeholder. That ADR names **this repo** as the
future real source of that data, via a message it calls (working title)
`PathCapacityChanged`, and says the record for that message belongs **here**,
in wes-work-planning, not in order-management. This ADR is that record.

### This repo already has everything the concept needs — it owns nothing new

`release.WorkPool` (`internal/domain/release/work_pool.go`) already tracks,
per process path:

- `mode` (`ReleaseFed` vs `FlowFed`) — release-fed pools enforce a hard
  `wipLimit` invariant on admission; flow-fed pools have no such ceiling,
  only an informative `alarmThreshold` (see
  [ADR-0003](./0003-flow-balancing-as-domain-service.md)).
- `WIP()` — the count of released-but-not-yet-completed entries, i.e. how
  much of `wipLimit` is currently occupied.

"Remaining admission capacity" is therefore **already computable** from
state this service holds today: `wipLimit - WIP()` for a `ReleaseFed` pool.
This ADR does not introduce a new domain concept, a new aggregate, or new
arithmetic — it **publishes internal state that has never been exposed
outside this service's own REST telemetry endpoint.**

### Why this repo, not order-management, and not a new context

order-management has no notion of a work pool, a WIP limit, or a release
policy — those are this service's aggregates. Building `PathCapacity`'s real
data source in order-management would mean either (a) order-management
reaching synchronously into this service's internals (violating the
hexagonal boundary order-management's own ADR 0014 Step A was designed to
keep clean via the port), or (b) duplicating WorkPool-shaped state in a
context that has no reason to own it. A brand-new bounded context was also
considered and rejected: there is no new domain concept here, just a new
publication of an existing one — a new context would own zero domain logic
of its own, which is precisely the kind of anemic service this fleet's own
DDD conventions warn against.

### The demand/supply split this repo already documents

`charge.ChargeForecast` (`internal/domain/charge/forecast.go`) already
models the **demand** side of the CPT-bucketed picture — `CPTBucket{CPT,
Quantity}`, volume due by CPT. `WorkPool.RemainingCapacity` (introduced by
this ADR) is the **supply** side: how much of that path's admission ceiling
is still free right now. The two live in different aggregates today
(`ChargeForecast` is keyed and bucketed by CPT explicitly; `WorkPool` is
keyed by path only, with individual entries each carrying their own CPT) and
this ADR does not merge them — it follows `WorkPool`'s own native shape
rather than inventing a new per-CPT-bucketed capacity aggregate that this
service's actual admission control (a single pool-wide `wipLimit`) does not
have.

## Decision

**We will raise a new domain event, `PathCapacityChanged`, from the existing
`SampleBacklog` use case when the caller supplies an optional CPT cutoff
timestamp, and publish it on the existing integration topic
(`warehouse.work-planning.events`) through the existing transactional
outbox.**

### 1. The event shape: `PathId`, `CutoffAt`, `RemainingUnits`, `Known`, `At`

```go
type PathCapacityChanged struct {
	baseEvent
	PathId         PathId
	CutoffAt       time.Time
	RemainingUnits int
	Known          bool
}
```

`CutoffAt` is a concrete `time.Time` — this service's own native CPT
currency (`shared.CPT` wraps exactly this), not a string identifier. See
"Correlation by timestamp, not by `cptId`" below for why.

### 2. The ReleaseFed/FlowFed `Known` rule, and why alarm-threshold-as-capacity was rejected

`WorkPool.RemainingCapacity()`:

- **`ReleaseFed` with `wipLimit > 0`**: `Known = true`,
  `RemainingUnits = max(0, wipLimit - WIP())`. This is a real ceiling this
  service already enforces as a domain invariant (`ErrWIPLimitReached`) — the
  most honest capacity figure this service can report.
- **`ReleaseFed` with `wipLimit <= 0`** (never provisioned for admission
  control): `Known = false`, `RemainingUnits = 0`. A literal `0` here would
  be indistinguishable from "genuinely saturated," which is worse than
  admitting the figure isn't meaningful.
- **`FlowFed`**: `Known = false`, `RemainingUnits = 0`, **always** —
  regardless of `alarmThreshold`.

The `FlowFed` case is the one substantive judgement call in this ADR, and it
is deliberately conservative. This repo's own
[ADR-0003](./0003-flow-balancing-as-domain-service.md) states the rationale
for flow-fed pools directly: *"A flow-fed pool cannot refuse arrivals — a
conveyor does not ask permission — so the only lever is upstream admission."*
`alarmThreshold` is a **backlog alarm** — a signal that triggers a
`ThrottleUpstream` recommendation — not an admission ceiling a caller can
plan a promise against. Backing out a "remaining capacity" figure from
`alarmThreshold - BacklogDepth()` was considered and rejected: it would hand
a downstream consumer (order-management's future promise calculation) a
number that *looks* like a hard constraint but is actually a soft,
after-the-fact alarm with no admission-refusal behavior behind it. Reporting
`Known=false` here is the more honest signal — order-management's own
`UnknownPathCapacity` placeholder already treats `known=false` as a
first-class, expected answer, so a flow-fed path degrading gracefully to
"unknown" costs the consumer nothing it doesn't already handle.

### 3. Trigger: extend `SampleBacklog`, not a new use case or a new cron

`SampleBacklog` already reads live `WorkPool` state and already
conditionally raises a telemetry event (`BacklogThresholdBreached`) without
saving anything, wrapped in `atomically()` per this repo's own documented
convention: *"use cases that publish without saving still wrap the Publish
so it commits atomically (trivial scope, but uniform)."* Extending it with
an optional `CutoffAt` field on `SampleBacklogRequest` — zero value skips
capacity reporting entirely — reuses that exact shape instead of adding:

- a new use case duplicating the pool lookup, or
- a new periodic sampler/cron. This repo has no existing background
  "publish current state" scheduler (the flow-balancing decision in
  ADR-0003 is explicitly evaluated on read, not swept), and adding one
  purely for this feature would be new operational surface for a v1 whose
  actual trigger — an on-demand REST read — is sufficient: `GET
  /paths/{pathId}/telemetry?cutoffAt=...` is exactly the kind of call a
  periodic external poller (the same poller pattern ADR-0003 already
  defers to for continuous rebalance monitoring) would make.

`GET /paths/{pathId}/telemetry` therefore gains an optional `cutoffAt` query
parameter (RFC3339). Supplying it makes the response additionally report
`remainingCapacityKnown`/`remainingCapacityUnits` and publishes
`PathCapacityChanged`, correlated against that timestamp. Omitting it (the
default) leaves every existing caller's behavior byte-for-byte unchanged.

### 4. Wiring: the same outbox pattern as every other publishing use case

`SampleBacklog.Execute` collects every event to raise (`BacklogThresholdBreached`
when over threshold, `PathCapacityChanged` when `CutoffAt` is supplied — both
can fire in the same call) into one slice and passes it through a single
`atomically()` call, so both commit or neither does — no new transactional
mechanism, following `RebalanceDecision`'s and `SampleBacklog`'s own existing
pattern (ADR-0014, transactional outbox). The outbound Kafka encoder
(`internal/adapters/outbound/kafka/publisher.go`) gains one more case in its
existing `dataFor` type switch, and the analytics fan-out
(`internal/adapters/outbound/kafka/analytics_publisher.go`) gains the
matching case in `marshalAnalyticsData`, exactly mirroring how every other
event type here is wired to both topics.

### 5. Correlation by `CutoffAt` timestamp, not by process-path-management's `cptId` string — the single most important design choice here

order-management's `ports.PathCapacity.Remaining` signature takes
`cptId string` — process-path-management's site-schedule cutoff identifier
(e.g. `"sp1-1500"`), cached in order-management via its `kafkacptschedule`
package (`NextCutoffs() []CPTWindow{CptId, CutoffAt, EligiblePathIds}`).
This repo has **zero awareness of process-path-management's CPTSchedule/cptId
concept today** (verified: no `cptschedule`/`CPTSchedule` references anywhere
in this repo). Two designs were on the table:

**Option A (rejected): consume process-path-management's `CPTSchedule` here
and correlate internally**, publishing `PathCapacityChanged` already keyed by
`cptId`, so order-management's future consumer needs no correlation logic at
all.

- Rejected because it gives wes-work-planning a **brand-new inbound
  dependency** (a Kafka consumer for process-path-management's schedule
  topic) that this service does not need for anything else it does. This
  service's own `.claude/rules/integration-events.md` documents exactly
  four consumed topics today (workforce, inventory, fulfillment,
  order-management); adding a fifth purely to translate an identifier this
  service otherwise has no use for is a dependency this repo would carry
  forever for one consumer's convenience. It also couples this event's
  publication cadence to process-path-management's schedule-topic
  availability — a new failure mode for a report that should degrade to
  "unknown," not to "blocked on a schedule sync."

**Option B (accepted): publish `PathId + CutoffAt (time.Time) +
RemainingUnits + Known`; the future order-management consumer correlates
`CutoffAt` against its own cached `CPTWindow.CutoffAt`** (exact match, or
nearest-window match within a tolerance the consumer defines) to resolve
which `cptId` this figure applies to.

- This keeps wes-work-planning's zero-cross-repo-dependency posture intact:
  `CutoffAt` is exactly this service's own native `shared.CPT`/`time.Time`
  currency — the same type `ChargeForecast.CPTBucket.CPT` and
  `WorkUnit.CPT()` already use everywhere in this codebase. Publishing it
  costs nothing new.
- **The honest cost, stated explicitly**: correlation-by-timestamp is
  imprecise in a way correlation-by-`cptId` is not. If process-path-management
  ever shifts a cutoff's `CutoffAt` by even a few minutes (a schedule
  amendment) without a compensating re-publish from this service, a
  timestamp match on the *old* cutoff time could miss the *new* window
  entirely, or a nearest-match heuristic could attribute a capacity figure
  to the wrong cutoff when two windows sit close together. This is an
  **accepted v1 trade, not an oversight**: the alternative (Option A) fixes
  this imprecision only by importing a whole new inbound dependency this
  service does not otherwise need, which is a permanent architectural cost
  to eliminate an edge case in a downstream consumer's matching logic. The
  correlation matching itself is order-management's problem to solve inside
  its own bounded context — it already owns the `CPTWindow` cache this
  match runs against, and it is squarely order-management's future PR (not
  this one) to implement `Remaining(pathId, cptId)` by resolving `cptId` to
  a `CutoffAt` and finding a wes-work-planning event whose `CutoffAt`
  matches (exactly or within a defined tolerance).
- This is explicitly **deferred, not deemed permanently sufficient**: if the
  nearest-match imprecision proves too coarse once real cadence data exists,
  a later ADR (in *either* repo) can revisit whether a stronger
  correlation key is worth the cross-repo coupling it would add. Nothing in
  this design blocks that later addition — `PathCapacityChanged`'s schema
  gains a field additively, exactly like `WorkReleased`'s
  `required_capabilities`/`fragile` optional-field precedent (ADR-0009).

### Alternatives considered and rejected

- **A brand-new bounded context for "capacity."** Rejected — no new domain
  logic exists to own; see "Why this repo" above.
- **Consume process-path-management's `CPTSchedule` directly** (Option A
  above). Rejected — new inbound dependency this service does not otherwise
  need; see "Correlation by `CutoffAt`" above.
- **Derive a FlowFed "remaining capacity" from `alarmThreshold`.** Rejected —
  misrepresents a backlog alarm as a hard admission ceiling; see "The
  ReleaseFed/FlowFed `Known` rule" above.
- **A new periodic sampler/cron publishing snapshots on a timer.** Rejected
  for v1 — no existing scheduler infrastructure in this repo to piggyback
  on, and an on-demand REST-triggered read (mirroring `RebalanceDecision`'s
  own "evaluated on read, not swept" philosophy from ADR-0003) is sufficient
  until a real consumer cadence requirement emerges.
- **A brand-new use case (`PublishPathCapacity`) instead of extending
  `SampleBacklog`.** Rejected — `SampleBacklog` already reads the exact pool
  state this event needs and already has the "publish-without-saving,
  atomically-wrapped" shape; a parallel use case would duplicate that
  wiring for no benefit.

## Consequences

### Easier

- **order-management's Step B (a real `PathCapacity` adapter) is now
  unblocked from this side.** A future `kafkaworkplanningcapacity`-style
  consumer in order-management can build directly against a real,
  documented wire contract instead of a placeholder.
- **Zero new cross-repo Go imports, either direction.** This repo still
  imports nothing from order-management or process-path-management; the
  only coupling is the wire-format contract in `apis/asyncapi.yaml`.
- **No new persisted state.** `RemainingCapacity()` is a pure read over
  already-persisted `WorkPool` fields; there is no new table, no new
  idempotency mechanism, and no new migration.
- **Reuses every existing safety net.** The transactional outbox
  (ADR-0014), the CloudEvents-vs-flat-envelope documentation split, and the
  omit-when-not-applicable field convention (ADR-0009, ADR-0010, ADR-0017)
  all apply here unchanged.

### Harder

- **`Known=false` on a FlowFed path is a real, permanent gap in v1, not a
  temporary placeholder.** A consumer wanting *any* capacity signal for a
  flow-fed path gets nothing from this event — by design, per the honesty
  argument above — until/unless a later ADR introduces a different signal
  for flow-fed admission (out of scope here).
- **Correlation-by-timestamp is imprecise**, as detailed above. A consumer
  that needs exact `cptId` attribution takes on that matching burden itself;
  this repo does not solve it.
- **No continuous publication without an external caller.** Exactly the
  same accepted gap ADR-0003 already names for `RebalanceDecision`: nothing
  here is detected or published unless someone calls `GET
  .../telemetry?cutoffAt=...`. A dashboard, operator tool, or scheduled
  poller must drive it from outside the core domain.
- **A capacity figure reported once is not retroactively corrected.**
  Exactly like ADR-0009's classification-drift gap and ADR-0017's
  travel-distance-staleness gap: if the pool's WIP changes a second after
  this event is raised, the previously published figure is stale until the
  next call. This is accepted for the same reason those two ADRs accept
  their equivalent staleness: it is a snapshot report, not a live
  subscription.
