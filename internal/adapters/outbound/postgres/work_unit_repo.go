package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/claudioed/wes-work-planning/internal/application/ports"
	"github.com/claudioed/wes-work-planning/internal/domain/shared"
	"github.com/claudioed/wes-work-planning/internal/domain/workunit"
)

type WorkUnitRepo struct {
	pool *pgxpool.Pool
}

func NewWorkUnitRepo(pool *pgxpool.Pool) *WorkUnitRepo {
	return &WorkUnitRepo{pool: pool}
}

func stateToString(s workunit.State) string {
	return s.String()
}

func (r *WorkUnitRepo) Save(ctx context.Context, unit *workunit.WorkUnit) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO work_units (id, path_id, cpt, reference, state, released_at, completed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (id) DO UPDATE SET
			path_id = $2, cpt = $3, reference = $4, state = $5, released_at = $6, completed_at = $7
	`, unit.Id(), unit.PathId().String(), unit.CPT().Time(), unit.Reference(), stateToString(unit.State()), unit.ReleasedAt(), unit.CompletedAt())
	return err
}

func (r *WorkUnitRepo) scanWorkUnit(id, pathIdStr, reference, state string, cpt time.Time, releasedAt, completedAt *time.Time) (*workunit.WorkUnit, error) {
	pathId, err := shared.NewPathId(pathIdStr)
	if err != nil {
		return nil, err
	}

	unit, err := workunit.NewWorkUnit(id, pathId, shared.NewCPT(cpt), reference)
	if err != nil {
		return nil, err
	}

	switch state {
	case workunit.Released.String():
		if err := unit.Release(*releasedAt); err != nil {
			return nil, err
		}
	case workunit.Completed.String():
		if err := unit.Release(*releasedAt); err != nil {
			return nil, err
		}
		if err := unit.Complete(*completedAt); err != nil {
			return nil, err
		}
	}

	return unit, nil
}

func (r *WorkUnitRepo) FindById(ctx context.Context, id string) (*workunit.WorkUnit, error) {
	var pathIdStr, reference, state string
	var cpt time.Time
	var releasedAt, completedAt *time.Time

	row := r.pool.QueryRow(ctx, `
		SELECT path_id, cpt, reference, state, released_at, completed_at
		FROM work_units WHERE id = $1
	`, id)
	if err := row.Scan(&pathIdStr, &cpt, &reference, &state, &releasedAt, &completedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ports.ErrNotFound
		}
		return nil, err
	}

	return r.scanWorkUnit(id, pathIdStr, reference, state, cpt, releasedAt, completedAt)
}

func (r *WorkUnitRepo) FindByPathId(ctx context.Context, pathId shared.PathId) ([]*workunit.WorkUnit, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, cpt, reference, state, released_at, completed_at
		FROM work_units WHERE path_id = $1
	`, pathId.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*workunit.WorkUnit
	for rows.Next() {
		var id, reference, state string
		var cpt time.Time
		var releasedAt, completedAt *time.Time
		if err := rows.Scan(&id, &cpt, &reference, &state, &releasedAt, &completedAt); err != nil {
			return nil, err
		}
		unit, err := r.scanWorkUnit(id, pathId.String(), reference, state, cpt, releasedAt, completedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, unit)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return out, nil
}
