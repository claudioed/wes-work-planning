package usecases

import (
	"context"
	"errors"

	"github.com/claudioed/wes-work-planning/internal/application/ports"
	"github.com/claudioed/wes-work-planning/internal/domain/release"
	"github.com/claudioed/wes-work-planning/internal/domain/shared"
	"github.com/claudioed/wes-work-planning/internal/domain/workunit"
)

// defaultWIPLimit and defaultAlarmThreshold seed a path's WorkPool the first
// time work is enqueued against it, if no pool has been provisioned yet.
const (
	defaultWIPLimit       = 1000
	defaultAlarmThreshold = 1000
)

// EnqueueWorkUnit adds a releasable unit of work to a path's work pool.
type EnqueueWorkUnit struct {
	workUnits ports.WorkUnitRepo
	pools     ports.WorkPoolRepo
	publisher ports.EventPublisher
	clock     ports.Clock
}

func NewEnqueueWorkUnit(workUnits ports.WorkUnitRepo, pools ports.WorkPoolRepo, publisher ports.EventPublisher, clock ports.Clock) *EnqueueWorkUnit {
	return &EnqueueWorkUnit{workUnits: workUnits, pools: pools, publisher: publisher, clock: clock}
}

type EnqueueWorkUnitRequest struct {
	WorkUnitId string
	PathId     shared.PathId
	CPT        shared.CPT
	Reference  string
}

func (uc *EnqueueWorkUnit) Execute(ctx context.Context, req EnqueueWorkUnitRequest) (*workunit.WorkUnit, error) {
	unit, err := workunit.NewWorkUnit(req.WorkUnitId, req.PathId, req.CPT, req.Reference)
	if err != nil {
		return nil, err
	}

	pool, err := uc.pools.FindByPathId(ctx, req.PathId)
	if errors.Is(err, ports.ErrNotFound) {
		pool = release.NewWorkPool(req.PathId, release.ReleaseFed, defaultWIPLimit, defaultAlarmThreshold)
	} else if err != nil {
		return nil, err
	}

	if err := pool.Enqueue(unit.Id(), unit.CPT()); err != nil {
		return nil, err
	}

	if err := uc.workUnits.Save(ctx, unit); err != nil {
		return nil, err
	}
	if err := uc.pools.Save(ctx, pool); err != nil {
		return nil, err
	}

	event := shared.NewWorkUnitCreated(unit.Id(), req.PathId, uc.clock.Now())
	if err := uc.publisher.Publish(ctx, event); err != nil {
		return nil, err
	}

	return unit, nil
}
