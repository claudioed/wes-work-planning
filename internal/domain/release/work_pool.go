package release

import "github.com/claudioed/wes-work-planning/internal/domain/shared"

// FeedMode distinguishes release-fed pools (WES controls admission volume via
// a WIP limit) from flow-fed pools (priority only; backlog is watched against
// an alarm threshold instead).
type FeedMode int

const (
	ReleaseFed FeedMode = iota
	FlowFed
)

type entryState int

const (
	pending entryState = iota
	released
)

type poolEntry struct {
	workUnitId string
	cpt        shared.CPT
	state      entryState
}

// WorkPool is the queue for one process path: backlog depth, arrival rate,
// service rate live here as behavior over enqueued entries. It hands out
// work in priority order (earliest CPT first), at most once per entry. The
// WIP limit is an enforceable invariant on release-fed pools; it is only an
// alarm threshold on flow-fed pools.
type WorkPool struct {
	pathId         shared.PathId
	mode           FeedMode
	wipLimit       int // only enforced when mode == ReleaseFed
	alarmThreshold int // only informative when mode == FlowFed
	entries        []poolEntry
}

// NewWorkPool constructs an empty WorkPool for a path.
func NewWorkPool(pathId shared.PathId, mode FeedMode, wipLimit, alarmThreshold int) *WorkPool {
	return &WorkPool{pathId: pathId, mode: mode, wipLimit: wipLimit, alarmThreshold: alarmThreshold}
}

func (p *WorkPool) PathId() shared.PathId { return p.pathId }
func (p *WorkPool) Mode() FeedMode        { return p.mode }
func (p *WorkPool) WIPLimit() int         { return p.wipLimit }
func (p *WorkPool) AlarmThreshold() int   { return p.alarmThreshold }

// PoolEntrySnapshot is a read-only projection of one pool entry, for
// adapters that need to persist or display pool contents.
type PoolEntrySnapshot struct {
	WorkUnitId string
	CPT        shared.CPT
	Released   bool
}

// Entries returns a snapshot of every entry currently in the pool.
func (p *WorkPool) Entries() []PoolEntrySnapshot {
	out := make([]PoolEntrySnapshot, len(p.entries))
	for i, e := range p.entries {
		out[i] = PoolEntrySnapshot{WorkUnitId: e.workUnitId, CPT: e.cpt, Released: e.state == released}
	}
	return out
}

// Enqueue adds a work unit to the pool as pending. Fails if already present.
func (p *WorkPool) Enqueue(workUnitId string, cpt shared.CPT) error {
	for _, e := range p.entries {
		if e.workUnitId == workUnitId {
			return ErrDuplicateEntry
		}
	}
	p.entries = append(p.entries, poolEntry{workUnitId: workUnitId, cpt: cpt, state: pending})
	return nil
}

// BacklogDepth is the count of pending (not yet released) entries.
func (p *WorkPool) BacklogDepth() int {
	count := 0
	for _, e := range p.entries {
		if e.state == pending {
			count++
		}
	}
	return count
}

// WIP is the count of released (outstanding) entries.
func (p *WorkPool) WIP() int {
	count := 0
	for _, e := range p.entries {
		if e.state == released {
			count++
		}
	}
	return count
}

// IsOverAlarmThreshold reports whether backlog depth exceeds the flow-fed
// alarm threshold. Meaningless for release-fed pools, which enforce the WIP
// limit as a hard invariant instead.
func (p *WorkPool) IsOverAlarmThreshold() bool {
	return p.BacklogDepth() > p.alarmThreshold
}

// ReleaseNext hands out the highest-priority pending entry (earliest CPT).
// On a release-fed pool, releasing beyond the WIP limit fails — this is the
// enforced invariant. Each entry can be released at most once.
func (p *WorkPool) ReleaseNext() (string, error) {
	idx := p.nextPendingIndex()
	if idx == -1 {
		return "", ErrEmptyPool
	}
	if p.mode == ReleaseFed && p.WIP() >= p.wipLimit {
		return "", ErrWIPLimitReached
	}
	p.entries[idx].state = released
	return p.entries[idx].workUnitId, nil
}

// nextPendingIndex finds the pending entry with the earliest (most urgent)
// CPT.
func (p *WorkPool) nextPendingIndex() int {
	best := -1
	for i, e := range p.entries {
		if e.state != pending {
			continue
		}
		if best == -1 || e.cpt.Before(p.entries[best].cpt) {
			best = i
		}
	}
	return best
}

// Release marks a specific entry as released. Fails if unknown or already
// released — enforcing at-most-once handout.
func (p *WorkPool) Release(workUnitId string) error {
	for i, e := range p.entries {
		if e.workUnitId != workUnitId {
			continue
		}
		if e.state == released {
			return ErrAlreadyReleased
		}
		if p.mode == ReleaseFed && p.WIP() >= p.wipLimit {
			return ErrWIPLimitReached
		}
		p.entries[i].state = released
		return nil
	}
	return ErrUnknownEntry
}
