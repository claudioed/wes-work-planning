package usecases

import (
	"context"

	"github.com/claudioed/wes-work-planning/internal/application/ports"
	"github.com/claudioed/wes-work-planning/internal/domain/shared"
	"github.com/claudioed/wes-work-planning/internal/domain/workunit"
)

// RecordCompletion finishes a released work unit and publishes
// WorkUnitCompleted so telemetry projections can update.
type RecordCompletion struct {
	workUnits ports.WorkUnitRepo
	publisher ports.EventPublisher
	clock     ports.Clock
}

func NewRecordCompletion(workUnits ports.WorkUnitRepo, publisher ports.EventPublisher, clock ports.Clock) *RecordCompletion {
	return &RecordCompletion{workUnits: workUnits, publisher: publisher, clock: clock}
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

	event := shared.NewWorkUnitCompleted(unit.Id(), unit.PathId(), now)
	if err := uc.publisher.Publish(ctx, event); err != nil {
		return nil, err
	}

	return unit, nil
}
