package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/claudioed/wes-work-planning/internal/application/ports"
	"github.com/claudioed/wes-work-planning/internal/domain/laborview"
	"github.com/claudioed/wes-work-planning/internal/domain/shared"
)

type LaborPlanViewRepo struct {
	pool *pgxpool.Pool
}

func NewLaborPlanViewRepo(pool *pgxpool.Pool) *LaborPlanViewRepo {
	return &LaborPlanViewRepo{pool: pool}
}

func (r *LaborPlanViewRepo) Save(ctx context.Context, view laborview.LaborPlanObserved) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO labor_plan_view (path_id, planned_heads, planned_rate, planned_hours, observed_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (path_id) DO UPDATE SET
			planned_heads = $2, planned_rate = $3, planned_hours = $4, observed_at = $5
	`, view.PathId.String(), view.PlannedHeads, view.PlannedRate, view.PlannedHours, view.ObservedAt)
	return err
}

func (r *LaborPlanViewRepo) FindByPathId(ctx context.Context, pathId shared.PathId) (laborview.LaborPlanObserved, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT planned_heads, planned_rate, planned_hours, observed_at
		FROM labor_plan_view WHERE path_id = $1
	`, pathId.String())

	var v laborview.LaborPlanObserved
	v.PathId = pathId
	if err := row.Scan(&v.PlannedHeads, &v.PlannedRate, &v.PlannedHours, &v.ObservedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return laborview.LaborPlanObserved{}, ports.ErrNotFound
		}
		return laborview.LaborPlanObserved{}, err
	}
	return v, nil
}
