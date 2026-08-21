package usecases

import (
	"context"

	"github.com/claudioed/wes-work-planning/internal/application/ports"
	"github.com/claudioed/wes-work-planning/internal/domain/release"
	"github.com/claudioed/wes-work-planning/internal/domain/shared"
	"github.com/claudioed/wes-work-planning/internal/domain/workunit"
)

// ReleaseNextWork applies the release policy to a path's work pool,
// admitting the next highest-priority (earliest CPT) pending work unit.
type ReleaseNextWork struct {
	pools     ports.WorkPoolRepo
	workUnits ports.WorkUnitRepo
	publisher ports.EventPublisher
	clock     ports.Clock
	policy    release.ReleasePolicy
}

func NewReleaseNextWork(pools ports.WorkPoolRepo, workUnits ports.WorkUnitRepo, publisher ports.EventPublisher, clock ports.Clock) *ReleaseNextWork {
	return &ReleaseNextWork{pools: pools, workUnits: workUnits, publisher: publisher, clock: clock, policy: release.NewReleasePolicy()}
}

type ReleaseNextWorkRequest struct {
	PathId shared.PathId
}

func (uc *ReleaseNextWork) Execute(ctx context.Context, req ReleaseNextWorkRequest) (*workunit.WorkUnit, error) {
	pool, err := uc.pools.FindByPathId(ctx, req.PathId)
	if err != nil {
		return nil, err
	}

	workUnitId, err := uc.policy.Apply(pool)
	if err != nil {
		return nil, err
	}

	unit, err := uc.workUnits.FindById(ctx, workUnitId)
	if err != nil {
		return nil, err
	}

	now := uc.clock.Now()
	if err := unit.Release(now); err != nil {
		return nil, err
	}

	if err := uc.pools.Save(ctx, pool); err != nil {
		return nil, err
	}
	if err := uc.workUnits.Save(ctx, unit); err != nil {
		return nil, err
	}

	event := shared.NewWorkReleased(unit.Id(), req.PathId, now)
	if err := uc.publisher.Publish(ctx, event); err != nil {
		return nil, err
	}

	return unit, nil
}
