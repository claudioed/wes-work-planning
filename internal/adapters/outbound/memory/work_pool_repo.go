package memory

import (
	"context"
	"sync"

	"github.com/claudioed/wes-work-planning/internal/application/ports"
	"github.com/claudioed/wes-work-planning/internal/domain/release"
	"github.com/claudioed/wes-work-planning/internal/domain/shared"
)

type WorkPoolRepo struct {
	mu     sync.RWMutex
	byPath map[string]*release.WorkPool
}

func NewWorkPoolRepo() *WorkPoolRepo {
	return &WorkPoolRepo{byPath: make(map[string]*release.WorkPool)}
}

func (r *WorkPoolRepo) Save(ctx context.Context, pool *release.WorkPool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byPath[pool.PathId().String()] = pool
	return nil
}

func (r *WorkPoolRepo) FindByPathId(ctx context.Context, pathId shared.PathId) (*release.WorkPool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.byPath[pathId.String()]
	if !ok {
		return nil, ports.ErrNotFound
	}
	return p, nil
}
