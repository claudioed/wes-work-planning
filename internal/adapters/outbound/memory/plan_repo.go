package memory

import (
	"context"
	"sync"

	"github.com/claudioed/wes-work-planning/internal/application/ports"
	"github.com/claudioed/wes-work-planning/internal/domain/plan"
	"github.com/claudioed/wes-work-planning/internal/domain/shared"
)

type PlanRepo struct {
	mu     sync.RWMutex
	byPath map[string]*plan.ShiftPlan
}

func NewPlanRepo() *PlanRepo {
	return &PlanRepo{byPath: make(map[string]*plan.ShiftPlan)}
}

func (r *PlanRepo) Save(ctx context.Context, pathId shared.PathId, shiftPlan *plan.ShiftPlan) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byPath[pathId.String()] = shiftPlan
	return nil
}

func (r *PlanRepo) FindByPathId(ctx context.Context, pathId shared.PathId) (*plan.ShiftPlan, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.byPath[pathId.String()]
	if !ok {
		return nil, ports.ErrNotFound
	}
	return p, nil
}
