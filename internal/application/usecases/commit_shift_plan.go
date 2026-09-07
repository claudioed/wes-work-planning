package usecases

import (
	"context"

	"github.com/claudioed/wes-work-planning/internal/application/ports"
	"github.com/claudioed/wes-work-planning/internal/domain/plan"
	"github.com/claudioed/wes-work-planning/internal/domain/shared"
)

// CommitShiftPlan commits the rate x heads x hours split for a process path,
// validating the plannedHeads <= installedStations invariant.
type CommitShiftPlan struct {
	plans     ports.PlanRepo
	publisher ports.EventPublisher
	clock     ports.Clock
	uow       ports.UnitOfWork
}

func NewCommitShiftPlan(plans ports.PlanRepo, publisher ports.EventPublisher, clock ports.Clock) *CommitShiftPlan {
	return &CommitShiftPlan{plans: plans, publisher: publisher, clock: clock}
}

// WithUnitOfWork brackets Save + Publish in one atomic scope (ADR-0014).
// Optional: nil keeps the two calls running back to back.
func (uc *CommitShiftPlan) WithUnitOfWork(u ports.UnitOfWork) *CommitShiftPlan {
	uc.uow = u
	return uc
}

type CommitShiftPlanRequest struct {
	PathId            shared.PathId
	PlannedHeads      shared.StationCount
	InstalledStations shared.StationCount
	Rate              shared.Rate
	Hours             float64
}

func (uc *CommitShiftPlan) Execute(ctx context.Context, req CommitShiftPlanRequest) (*plan.ShiftPlan, error) {
	pathPlan, err := plan.NewPathPlan(req.PathId, req.PlannedHeads, req.InstalledStations, req.Rate, req.Hours)
	if err != nil {
		return nil, err
	}

	shiftPlan, err := plan.NewShiftPlan([]plan.PathPlan{pathPlan})
	if err != nil {
		return nil, err
	}

	event := shared.NewShiftPlanCommitted(req.PathId, uc.clock.Now())
	err = atomically(ctx, uc.uow, func(ctx context.Context) error {
		if err := uc.plans.Save(ctx, req.PathId, shiftPlan); err != nil {
			return err
		}
		return uc.publisher.Publish(ctx, event)
	})
	if err != nil {
		return nil, err
	}

	return shiftPlan, nil
}
