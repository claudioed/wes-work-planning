package usecases

import (
	"context"

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
}

// SampleBacklog returns the current backlog depth/WIP read model for a path
// and raises a BacklogThresholdBreached event when the pool is over its
// alarm threshold.
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

	if snapshot.OverAlarmThreshold {
		event := shared.NewBacklogThresholdBreached(req.PathId, uc.clock.Now())
		err := atomically(ctx, uc.uow, func(ctx context.Context) error {
			return uc.publisher.Publish(ctx, event)
		})
		if err != nil {
			return BacklogSnapshot{}, err
		}
	}

	return snapshot, nil
}
