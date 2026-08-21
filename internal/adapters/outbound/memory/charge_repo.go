// Package memory provides thread-safe in-memory implementations of every
// outbound port, for tests and local development.
package memory

import (
	"context"
	"sync"

	"github.com/claudioed/wes-work-planning/internal/application/ports"
	"github.com/claudioed/wes-work-planning/internal/domain/charge"
	"github.com/claudioed/wes-work-planning/internal/domain/shared"
)

type ChargeRepo struct {
	mu     sync.RWMutex
	byPath map[string]*charge.ChargeForecast
}

func NewChargeRepo() *ChargeRepo {
	return &ChargeRepo{byPath: make(map[string]*charge.ChargeForecast)}
}

func (r *ChargeRepo) Save(ctx context.Context, forecast *charge.ChargeForecast) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byPath[forecast.PathId().String()] = forecast
	return nil
}

func (r *ChargeRepo) FindByPathId(ctx context.Context, pathId shared.PathId) (*charge.ChargeForecast, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	f, ok := r.byPath[pathId.String()]
	if !ok {
		return nil, ports.ErrNotFound
	}
	return f, nil
}
