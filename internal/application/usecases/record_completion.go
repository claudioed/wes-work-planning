package usecases

import (
	"context"

	"github.com/claudioed/wes-work-planning/internal/application/ports"
	"github.com/claudioed/wes-work-planning/internal/domain/shared"
	"github.com/claudioed/wes-work-planning/internal/domain/workunit"
)

// RecordCompletion finishes a released work unit and publishes
// WorkUnitCompleted so telemetry projections can update. It also frees the
// unit's WIP slot on its release-fed work pool (WorkPool.Complete) — the
// other half of the release/complete cycle. Without this, a release-fed
// pool's WIP count only ever rises: ReleaseNextWork marks an entry
// "released" and it never leaves that state, so once wipLimit entries have
// EVER been released the pool is permanently wedged shut regardless of how
// much of that work has since finished downstream (found via the e2e
// soak_backlog_ramp scenario: a 1h sustained run hit this ceiling in ~18
// minutes and every /release call 409'd for the rest of the run even
// though fulfillment-execution kept completing tasks with zero Kafka lag).
type RecordCompletion struct {
	workUnits ports.WorkUnitRepo
	pools     ports.WorkPoolRepo
	publisher ports.EventPublisher
	clock     ports.Clock
}

func NewRecordCompletion(workUnits ports.WorkUnitRepo, pools ports.WorkPoolRepo, publisher ports.EventPublisher, clock ports.Clock) *RecordCompletion {
	return &RecordCompletion{workUnits: workUnits, pools: pools, publisher: publisher, clock: clock}
}

type RecordCompletionRequest struct {
	WorkUnitId string
}

func (uc *RecordCompletion) Execute(ctx context.Context, req RecordCompletionRequest) (*workunit.WorkUnit, error) {
	unit, err := uc.workUnits.FindById(ctx, req.WorkUnitId)
	if err != nil {
		return nil, err
	}

	now := uc.clock.Now()
	if err := unit.Complete(now); err != nil {
		return nil, err
	}

	if err := uc.workUnits.Save(ctx, unit); err != nil {
		return nil, err
	}

	// Free this unit's WIP slot on its work pool. Best-effort against a
	// pool that predates this fix or was never release-fed for this path
	// (ErrNotFound): the WorkUnit's own completion above is the source of
	// truth and must not be rolled back just because the pool side-effect
	// couldn't be applied.
	if pool, err := uc.pools.FindByPathId(ctx, unit.PathId()); err == nil {
		if err := pool.Complete(unit.Id()); err == nil {
			if err := uc.pools.Save(ctx, pool); err != nil {
				return nil, err
			}
		}
	} else if err != ports.ErrNotFound {
		return nil, err
	}

	event := shared.NewWorkUnitCompleted(unit.Id(), unit.PathId(), now)
	if err := uc.publisher.Publish(ctx, event); err != nil {
		return nil, err
	}

	return unit, nil
}
