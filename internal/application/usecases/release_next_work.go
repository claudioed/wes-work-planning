package usecases

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

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
	released  metric.Int64Counter
	uow       ports.UnitOfWork
}

func NewReleaseNextWork(pools ports.WorkPoolRepo, workUnits ports.WorkUnitRepo, publisher ports.EventPublisher, clock ports.Clock) *ReleaseNextWork {
	return &ReleaseNextWork{
		pools:     pools,
		workUnits: workUnits,
		publisher: publisher,
		clock:     clock,
		policy:    release.NewReleasePolicy(),
		released: newInt64Counter("wes.work_units.released",
			metric.WithDescription("Work units admitted into a process path's work pool by the release policy."),
			metric.WithUnit("{work_unit}"),
		),
	}
}

type ReleaseNextWorkRequest struct {
	PathId shared.PathId
}

// WithUnitOfWork brackets both Saves + Publish in one atomic scope
// (ADR-0014). Optional: nil keeps the calls running back to back.
func (uc *ReleaseNextWork) WithUnitOfWork(u ports.UnitOfWork) *ReleaseNextWork {
	uc.uow = u
	return uc
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

	// The WorkUnit Save precedes Publish INSIDE the scope on purpose: the
	// integration publisher enriches WorkReleased by reading the work unit
	// back, and under the outbox that read must see this transaction's row.
	event := shared.NewWorkReleased(unit.Id(), req.PathId, now)
	err = atomically(ctx, uc.uow, func(ctx context.Context) error {
		if err := uc.pools.Save(ctx, pool); err != nil {
			return err
		}
		if err := uc.workUnits.Save(ctx, unit); err != nil {
			return err
		}
		return uc.publisher.Publish(ctx, event)
	})
	if err != nil {
		return nil, err
	}

	// Counted here rather than in the HTTP handler so the metric tracks the
	// real domain event — a work unit actually released — not the request.
	uc.released.Add(ctx, 1, metric.WithAttributes(attribute.String(AttrPathId, req.PathId.String())))

	return unit, nil
}
