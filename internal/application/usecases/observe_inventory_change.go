package usecases

import (
	"context"
	"time"

	"github.com/claudioed/wes-work-planning/internal/application/ports"
	"github.com/claudioed/wes-work-planning/internal/domain/inventoryview"
)

// ObserveInventoryChange is an additive projector use case: it applies
// Inventory's StockReserved/ReservationRevoked integration events to the
// UsableInventoryObserved read model, keyed by SKU.
type ObserveInventoryChange struct {
	views     ports.InventoryViewRepo
	processed ports.ProcessedEventRepo
}

func NewObserveInventoryChange(views ports.InventoryViewRepo, processed ports.ProcessedEventRepo) *ObserveInventoryChange {
	return &ObserveInventoryChange{views: views, processed: processed}
}

type ObserveInventoryChangeRequest struct {
	EventId    string
	SKU        string
	Quantity   int
	Delta      int // -Quantity for StockReserved, +Quantity for ReservationRevoked
	ObservedAt time.Time
}

func (uc *ObserveInventoryChange) Execute(ctx context.Context, req ObserveInventoryChangeRequest) (inventoryview.UsableInventoryObserved, error) {
	alreadyProcessed, err := uc.processed.TryMarkProcessed(ctx, req.EventId, req.ObservedAt)
	if err != nil {
		return inventoryview.UsableInventoryObserved{}, err
	}
	if alreadyProcessed {
		return uc.views.FindBySKU(ctx, req.SKU)
	}

	return uc.views.ApplyDelta(ctx, req.SKU, req.Delta, req.ObservedAt)
}
