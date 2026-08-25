package ports

import (
	"context"
	"time"

	"github.com/claudioed/wes-work-planning/internal/domain/inventoryview"
	"github.com/claudioed/wes-work-planning/internal/domain/laborview"
	"github.com/claudioed/wes-work-planning/internal/domain/productclassificationview"
	"github.com/claudioed/wes-work-planning/internal/domain/shared"
)

// LaborPlanViewRepo persists the LaborPlanObserved read model, one per path,
// projected from Workforce Management's ShiftPlanCommitted events.
type LaborPlanViewRepo interface {
	Save(ctx context.Context, view laborview.LaborPlanObserved) error
	FindByPathId(ctx context.Context, pathId shared.PathId) (laborview.LaborPlanObserved, error)
}

// InventoryViewRepo persists the UsableInventoryObserved read model, one per
// SKU, projected from Inventory's StockReserved/ReservationRevoked events.
type InventoryViewRepo interface {
	// ApplyDelta atomically adjusts the usable quantity for sku by delta,
	// creating the row (starting from zero) if absent, and returns the
	// updated view.
	ApplyDelta(ctx context.Context, sku string, delta int, observedAt time.Time) (inventoryview.UsableInventoryObserved, error)
	FindBySKU(ctx context.Context, sku string) (inventoryview.UsableInventoryObserved, error)
}

// ProcessedEventRepo tracks integration event IDs already applied to a read
// model, so at-least-once Kafka redelivery does not double-apply effects.
type ProcessedEventRepo interface {
	// TryMarkProcessed records eventId as processed if it hasn't been seen
	// before. It returns alreadyProcessed=true (and applies no state change)
	// when the event was already recorded.
	TryMarkProcessed(ctx context.Context, eventId string, processedAt time.Time) (alreadyProcessed bool, err error)
}

// ProductClassificationLookup is the outbound port for the synchronous
// cross-context read from inventory-storage's product-classification
// endpoint (GET /products/{sku}/classification), used at work-release time
// to stamp derived hazmat/fragile hints onto the published WorkReleased
// event without a live callback from fulfillment-execution (see ADR-0009).
//
// This is a synchronous HTTP read, not a Kafka projection, because
// inventory-storage's own outbound Kafka publisher does not forward
// ProductClassified to the broker — see the productclassificationview
// package doc comment for the evidence.
type ProductClassificationLookup interface {
	GetClassification(ctx context.Context, sku string) (productclassificationview.ProductClassificationView, error)
}
