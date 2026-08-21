package memory

import (
	"context"
	"sync"

	"github.com/claudioed/wes-work-planning/internal/application/ports"
	"github.com/claudioed/wes-work-planning/internal/domain/shared"
	"github.com/claudioed/wes-work-planning/internal/domain/workunit"
)

type WorkUnitRepo struct {
	mu   sync.RWMutex
	byId map[string]*workunit.WorkUnit
}

func NewWorkUnitRepo() *WorkUnitRepo {
	return &WorkUnitRepo{byId: make(map[string]*workunit.WorkUnit)}
}

func (r *WorkUnitRepo) Save(ctx context.Context, unit *workunit.WorkUnit) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byId[unit.Id()] = unit
	return nil
}

func (r *WorkUnitRepo) FindById(ctx context.Context, id string) (*workunit.WorkUnit, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	u, ok := r.byId[id]
	if !ok {
		return nil, ports.ErrNotFound
	}
	return u, nil
}

func (r *WorkUnitRepo) FindByPathId(ctx context.Context, pathId shared.PathId) ([]*workunit.WorkUnit, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*workunit.WorkUnit
	for _, u := range r.byId {
		if u.PathId().Equals(pathId) {
			out = append(out, u)
		}
	}
	return out, nil
}
