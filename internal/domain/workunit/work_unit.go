package workunit

import (
	"time"

	"github.com/claudioed/wes-work-planning/internal/domain/shared"
)

// State is the lifecycle stage of a WorkUnit.
type State int

const (
	Pending State = iota
	Released
	Completed
)

func (s State) String() string {
	switch s {
	case Pending:
		return "Pending"
	case Released:
		return "Released"
	case Completed:
		return "Completed"
	default:
		return "Unknown"
	}
}

// WorkUnit is a releasable unit of work (e.g. a pick task). It is assigned
// at most once at a time and cannot complete twice.
type WorkUnit struct {
	id          string
	pathId      shared.PathId
	cpt         shared.CPT
	reference   string
	sku         string
	state       State
	releasedAt  *time.Time
	completedAt *time.Time
}

// NewWorkUnit constructs a Pending WorkUnit for a path, carrying its CPT and
// an external reference (e.g. the pick task's source order line).
func NewWorkUnit(id string, pathId shared.PathId, cpt shared.CPT, reference string) (*WorkUnit, error) {
	if id == "" {
		return nil, ErrEmptyId
	}
	if reference == "" {
		return nil, ErrEmptyReference
	}
	return &WorkUnit{id: id, pathId: pathId, cpt: cpt, reference: reference, state: Pending}, nil
}

func (w *WorkUnit) Id() string            { return w.id }
func (w *WorkUnit) PathId() shared.PathId { return w.pathId }
func (w *WorkUnit) CPT() shared.CPT       { return w.cpt }
func (w *WorkUnit) Reference() string     { return w.reference }
func (w *WorkUnit) State() State          { return w.state }

// SKU is the optional inventory SKU this work unit's order line
// corresponds to. Empty when the caller did not supply one (e.g. a
// non-pick process path, or an order line that predates this field) —
// callers must treat "" as "no SKU known", not as an error.
//
// It exists solely so the outbound WorkReleased publisher can look up the
// SKU's ProductClassification once, at release time, and stamp derived
// hazmat/fragile hints onto the published event (see ADR-0009). Nothing in
// this aggregate's own invariants depends on it.
func (w *WorkUnit) SKU() string { return w.sku }

// SetSKU records the optional SKU after construction. A separate setter,
// rather than a NewWorkUnit parameter, keeps every existing caller and test
// fixture compiling unchanged — SKU is additive, not a new invariant.
func (w *WorkUnit) SetSKU(sku string) { w.sku = sku }

// Release admits the unit into active work. A unit may be assigned/released
// at most once — releasing an already-released or completed unit fails.
func (w *WorkUnit) Release(at time.Time) error {
	if w.state != Pending {
		return ErrAlreadyReleased
	}
	w.state = Released
	w.releasedAt = &at
	return nil
}

// Complete finishes a released unit. A unit cannot complete twice, and must
// be released before it can complete.
func (w *WorkUnit) Complete(at time.Time) error {
	if w.state == Completed {
		return ErrAlreadyCompleted
	}
	if w.state != Released {
		return ErrNotReleased
	}
	w.state = Completed
	w.completedAt = &at
	return nil
}

func (w *WorkUnit) ReleasedAt() *time.Time  { return w.releasedAt }
func (w *WorkUnit) CompletedAt() *time.Time { return w.completedAt }
