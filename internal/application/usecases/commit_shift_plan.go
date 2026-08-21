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
}

func NewCommitShiftPlan(plans ports.PlanRepo, publisher ports.EventPublisher, clock ports.Clock) *CommitShiftPlan {
	return &CommitShiftPlan{plans: plans, publisher: publisher, clock: clock}
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

	if err := uc.plans.Save(ctx, req.PathId, shiftPlan); err != nil {
		return nil, err
	}

	event := shared.NewShiftPlanCommitted(req.PathId, uc.clock.Now())
	if err := uc.publisher.Publish(ctx, event); err != nil {
		return nil, err
	}

	return shiftPlan, nil
}
