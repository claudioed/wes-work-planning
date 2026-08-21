package memory

import (
	"context"
	"sync"
	"time"

	"github.com/claudioed/wes-work-planning/internal/application/ports"
	"github.com/claudioed/wes-work-planning/internal/domain/inventoryview"
)

// InventoryViewRepo is a thread-safe in-memory ports.InventoryViewRepo.
type InventoryViewRepo struct {
	mu    sync.Mutex
	bySKU map[string]inventoryview.UsableInventoryObserved
}

func NewInventoryViewRepo() *InventoryViewRepo {
	return &InventoryViewRepo{bySKU: make(map[string]inventoryview.UsableInventoryObserved)}
}

func (r *InventoryViewRepo) ApplyDelta(ctx context.Context, sku string, delta int, observedAt time.Time) (inventoryview.UsableInventoryObserved, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	v := r.bySKU[sku]
	v.SKU = sku
	v.UsableQuantity += delta
	v.ObservedAt = observedAt
	r.bySKU[sku] = v
	return v, nil
}

func (r *InventoryViewRepo) FindBySKU(ctx context.Context, sku string) (inventoryview.UsableInventoryObserved, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.bySKU[sku]
	if !ok {
		return inventoryview.UsableInventoryObserved{}, ports.ErrNotFound
	}
	return v, nil
}
