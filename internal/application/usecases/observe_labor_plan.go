package usecases

import (
	"context"
	"time"

	"github.com/claudioed/wes-work-planning/internal/application/ports"
	"github.com/claudioed/wes-work-planning/internal/domain/laborview"
	"github.com/claudioed/wes-work-planning/internal/domain/shared"
)

// ObserveLaborPlan is an additive projector use case: it records the latest
// labor plan Workforce Management reports for a path via its own
// ShiftPlanCommitted integration event. This is a read-only projection —
// it must NOT feed this service's own plan.ShiftPlan/CommitShiftPlan.
type ObserveLaborPlan struct {
	views     ports.LaborPlanViewRepo
	processed ports.ProcessedEventRepo
}

func NewObserveLaborPlan(views ports.LaborPlanViewRepo, processed ports.ProcessedEventRepo) *ObserveLaborPlan {
	return &ObserveLaborPlan{views: views, processed: processed}
}

type ObserveLaborPlanRequest struct {
	EventId      string
	PathId       shared.PathId
	PlannedHeads int
	PlannedRate  float64
	PlannedHours float64
	ObservedAt   time.Time
}

func (uc *ObserveLaborPlan) Execute(ctx context.Context, req ObserveLaborPlanRequest) error {
	alreadyProcessed, err := uc.processed.TryMarkProcessed(ctx, req.EventId, req.ObservedAt)
	if err != nil {
		return err
	}
	if alreadyProcessed {
		return nil
	}

	return uc.views.Save(ctx, laborview.LaborPlanObserved{
		PathId:       req.PathId,
		PlannedHeads: req.PlannedHeads,
		PlannedRate:  req.PlannedRate,
		PlannedHours: req.PlannedHours,
		ObservedAt:   req.ObservedAt,
	})
}
