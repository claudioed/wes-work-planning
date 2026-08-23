---
id: why-waveless-release
title: Why waveless release
sidebar_label: Why waveless release
sidebar_position: 3
description: The reasoning behind continuous, priority-ordered admission instead of wave-based batching.
---

# Why waveless release

**Wave-based** release batches work into discrete groups, opens a wave, and
does not open the next one until the current wave closes. **Waveless** release
admits one unit at a time, continuously, in priority order, whenever there is
room.

This service is waveless. Release is a **policy applied on demand**
(`ReleasePolicy.Apply(pool)`), not a scheduled job that fires every N minutes.

## The problem with waves

### Waves are bursty by construction

A wave floods the floor at open and starves it at close. Average utilisation
looks fine; instantaneous utilisation oscillates between congestion and idle.
Neither extreme clears volume — congestion adds travel and queueing time, idle
adds nothing at all.

### The wave boundary is a synchronisation barrier you did not ask for

The reference model calls out the classic legacy behaviour: a wave-based system
closes the whole wave before opening packing. That is a barrier across
*unrelated* work — a slow line in one aisle holds up parcels that share nothing
with it except the accident of having been batched together.

### A wave cannot react to a CPT that moved

The moment a wave is composed, its priority ordering is frozen. If a truck is
re-timed, a path goes down, or a hot order arrives, the wave has no way to
respond — the correct next unit is already inside a batch that will not
re-sort. Under continuous release, the "next unit" is re-decided at every
single release call, so a changed CPT changes the very next handout.

## What continuous release buys

| Property | Wave | Waveless (this service) |
|---|---|---|
| Priority freshness | Frozen at wave composition | Re-evaluated at every release |
| Floor loading | Burst / starve cycle | Smooth, bounded by WIP limit |
| Reaction to a changed CPT | Next wave, at best | Next release call |
| Coupling between unrelated work | Whole wave synchronises | None |
| Backpressure mechanism | Wave size | WIP limit, per pool |

Continuous release turns "how much work should be on the floor" from a
*batching* question into a *backpressure* question, and backpressure is
something you can enforce as an invariant.

## How it actually works here

`WorkPool.ReleaseNext()` is the whole algorithm:

1. Find the **pending** entry with the **earliest CPT**. That is the priority
   function — there is no secondary sort, no age factor, no weighting. CPT is
   the deadline that physically exists; anything else would be a proxy for it.
2. If the pool is **release-fed** and WIP is already at the WIP limit, refuse
   with `ErrWIPLimitReached`. This is the backpressure.
3. Otherwise mark that entry released and hand out its id — **at most once**,
   enforced by the entry's own state.

```go
func (p *WorkPool) ReleaseNext() (string, error) {
    idx := p.nextPendingIndex()          // earliest-CPT pending entry
    if idx == -1 {
        return "", ErrEmptyPool
    }
    if p.mode == ReleaseFed && p.WIP() >= p.wipLimit {
        return "", ErrWIPLimitReached    // backpressure, not a queue
    }
    p.entries[idx].state = released
    return p.entries[idx].workUnitId, nil
}
```

Note what is *absent*: no timer, no batch size, no wave id, no cron. The
"continuous" in continuous release means the decision has no schedule of its
own — it happens whenever the floor asks for work.

## The cost, stated honestly

Waveless is not free:

- **No natural batching for travel.** A wave can deliberately group picks in
  the same aisle. Continuous release, ordered purely by CPT, cannot — that
  optimisation, where it is wanted, has to happen downstream in execution where
  travel is actually known.
- **Priority is single-dimensional.** Earliest-CPT-first is simple and
  defensible, but it will not, on its own, express "this customer tier first"
  or "cold-chain first". Adding those means changing the release policy, which
  is precisely why the policy is a separate domain-service object rather than
  a method buried in the pool.
- **More release calls.** One decision per unit instead of one per wave. Cheap
  here because the decision is pure in-memory domain logic, but it is a real
  difference in call volume.

The decision and its consequences are recorded in
[ADR-0002](../adr/0002-waveless-continuous-release.md).
