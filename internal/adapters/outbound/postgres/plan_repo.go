package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/claudioed/wes-work-planning/internal/application/ports"
	"github.com/claudioed/wes-work-planning/internal/domain/plan"
	"github.com/claudioed/wes-work-planning/internal/domain/shared"
)

type PlanRepo struct {
	pool *pgxpool.Pool
}

func NewPlanRepo(pool *pgxpool.Pool) *PlanRepo {
	return &PlanRepo{pool: pool}
}

func (r *PlanRepo) Save(ctx context.Context, pathId shared.PathId, shiftPlan *plan.ShiftPlan) error {
	pathPlan, ok := shiftPlan.PathPlan(pathId)
	if !ok {
		return errors.New("shift plan has no path plan for the given path id")
	}

	_, err := r.pool.Exec(ctx, `
		INSERT INTO shift_plans (path_id, planned_heads, installed_stations, rate_units_per_hr, hours)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (path_id) DO UPDATE SET
			planned_heads = $2, installed_stations = $3, rate_units_per_hr = $4, hours = $5
	`, pathId.String(), pathPlan.PlannedHeads().Value(), pathPlan.InstalledStations().Value(), pathPlan.Rate().UnitsPerHour(), pathPlan.Hours())
	return err
}

func (r *PlanRepo) FindByPathId(ctx context.Context, pathId shared.PathId) (*plan.ShiftPlan, error) {
	var plannedHeads, installedStations int
	var rateUnitsPerHr, hours float64

	row := r.pool.QueryRow(ctx, `
		SELECT planned_heads, installed_stations, rate_units_per_hr, hours
		FROM shift_plans WHERE path_id = $1
	`, pathId.String())
	if err := row.Scan(&plannedHeads, &installedStations, &rateUnitsPerHr, &hours); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ports.ErrNotFound
		}
		return nil, err
	}

	heads, err := shared.NewStationCount(plannedHeads)
	if err != nil {
		return nil, err
	}
	installed, err := shared.NewStationCount(installedStations)
	if err != nil {
		return nil, err
	}
	rate, err := shared.NewRate(rateUnitsPerHr)
	if err != nil {
		return nil, err
	}

	pathPlan, err := plan.NewPathPlan(pathId, heads, installed, rate, hours)
	if err != nil {
		return nil, err
	}

	return plan.NewShiftPlan([]plan.PathPlan{pathPlan})
}
