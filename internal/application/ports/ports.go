// Package ports defines the driven (outbound) port interfaces the
// application layer depends on: repositories, event publishing, and clock.
package ports

import (
	"context"
	"time"

	"github.com/claudioed/wes-work-planning/internal/domain/charge"
	"github.com/claudioed/wes-work-planning/internal/domain/plan"
	"github.com/claudioed/wes-work-planning/internal/domain/release"
	"github.com/claudioed/wes-work-planning/internal/domain/shared"
	"github.com/claudioed/wes-work-planning/internal/domain/workunit"
)

// ChargeRepo persists and retrieves ChargeForecast aggregates, one per path.
type ChargeRepo interface {
	Save(ctx context.Context, forecast *charge.ChargeForecast) error
	FindByPathId(ctx context.Context, pathId shared.PathId) (*charge.ChargeForecast, error)
}

// PlanRepo persists and retrieves ShiftPlan aggregates, one per path.
type PlanRepo interface {
	Save(ctx context.Context, pathId shared.PathId, shiftPlan *plan.ShiftPlan) error
	FindByPathId(ctx context.Context, pathId shared.PathId) (*plan.ShiftPlan, error)
}

// WorkPoolRepo persists and retrieves WorkPool aggregates, one per path.
type WorkPoolRepo interface {
	Save(ctx context.Context, pool *release.WorkPool) error
	FindByPathId(ctx context.Context, pathId shared.PathId) (*release.WorkPool, error)
}

// WorkUnitRepo persists and retrieves WorkUnit aggregates.
type WorkUnitRepo interface {
	Save(ctx context.Context, unit *workunit.WorkUnit) error
	FindById(ctx context.Context, id string) (*workunit.WorkUnit, error)
	FindByPathId(ctx context.Context, pathId shared.PathId) ([]*workunit.WorkUnit, error)
}

// EventPublisher publishes domain events raised by use cases.
type EventPublisher interface {
	Publish(ctx context.Context, events ...shared.DomainEvent) error
}

// Clock abstracts "now" so use cases and tests are deterministic.
type Clock interface {
	Now() time.Time
}
