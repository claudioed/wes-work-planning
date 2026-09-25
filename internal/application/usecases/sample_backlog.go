package usecases

import (
	"context"
	"time"

	"github.com/claudioed/wes-work-planning/internal/application/ports"
	"github.com/claudioed/wes-work-planning/internal/domain/release"
	"github.com/claudioed/wes-work-planning/internal/domain/shared"
)

// BacklogSnapshot is the SampleBacklog read model: a projection built from
// current pool state, not aggregate state.
type BacklogSnapshot struct {
	PathId             shared.PathId
	BacklogDepth       int
	WIP                int
	Mode               string
	OverAlarmThreshold bool
	// RemainingCapacityKnown/RemainingCapacityUnits are populated only
	// when the caller supplied a CutoffAt on the request (see
	// SampleBacklogRequest.CutoffAt) — this is the read-side mirror of
	// the PathCapacityChanged event raised in that case (ADR-0018).
	// Both are zero-valued when CutoffAt was not supplied.
	RemainingCapacityKnown bool
	RemainingCapacityUnits int
}

// SampleBacklog returns the current backlog depth/WIP read model for a path
// and raises a BacklogThresholdBreached event when the pool is over its
// alarm threshold.
//
// When the caller also supplies a CutoffAt (ADR-0018), it additionally
// computes the pool's current remaining admission capacity
// (release.WorkPool.RemainingCapacity) and raises PathCapacityChanged,
// correlating that figure against the given CPT cutoff timestamp. This
// reuses the existing sampling use case rather than adding a second
// trigger, per this repo's existing convention that a use case which
// publishes without saving still wraps the Publish in one atomic scope
// (ADR-0014) even when the scope is trivial.
type SampleBacklog struct {
	pools     ports.WorkPoolRepo
	publisher ports.EventPublisher
	clock     ports.Clock
	uow       ports.UnitOfWork
}

func NewSampleBacklog(pools ports.WorkPoolRepo, publisher ports.EventPublisher, clock ports.Clock) *SampleBacklog {
	return &SampleBacklog{pools: pools, publisher: publisher, clock: clock}
}

// WithUnitOfWork commits the Publish in one atomic scope (ADR-0014). This
// use case saves nothing, so the scope is trivial — kept uniform with every
// other publishing use case. Optional: nil publishes directly.
func (uc *SampleBacklog) WithUnitOfWork(u ports.UnitOfWork) *SampleBacklog {
	uc.uow = u
	return uc
}

type SampleBacklogRequest struct {
	PathId shared.PathId
	// CutoffAt is optional: when non-zero, the caller is asking this
	// service to report — and publish (ADR-0018) — the path's current
	// remaining admission capacity, correlated against this CPT cutoff
	// timestamp. Zero value (the default) skips capacity reporting
	// entirely, so every existing caller of SampleBacklog keeps
	// compiling and behaving unchanged.
	CutoffAt time.Time
}

func (uc *SampleBacklog) Execute(ctx context.Context, req SampleBacklogRequest) (BacklogSnapshot, error) {
	pool, err := uc.pools.FindByPathId(ctx, req.PathId)
	if err != nil {
		return BacklogSnapshot{}, err
	}

	modeName := "ReleaseFed"
	if pool.Mode() == release.FlowFed {
		modeName = "FlowFed"
	}

	snapshot := BacklogSnapshot{
		PathId:             req.PathId,
		BacklogDepth:       pool.BacklogDepth(),
		WIP:                pool.WIP(),
		Mode:               modeName,
		OverAlarmThreshold: pool.IsOverAlarmThreshold(),
	}

	var events []shared.DomainEvent
	if snapshot.OverAlarmThreshold {
		events = append(events, shared.NewBacklogThresholdBreached(req.PathId, uc.clock.Now()))
	}
	if !req.CutoffAt.IsZero() {
		remaining, known := pool.RemainingCapacity()
		snapshot.RemainingCapacityKnown = known
		snapshot.RemainingCapacityUnits = remaining
		events = append(events, shared.NewPathCapacityChanged(req.PathId, req.CutoffAt, remaining, known, uc.clock.Now()))
	}

	if len(events) > 0 {
		err := atomically(ctx, uc.uow, func(ctx context.Context) error {
			return uc.publisher.Publish(ctx, events...)
		})
		if err != nil {
			return BacklogSnapshot{}, err
		}
	}

	return snapshot, nil
}
