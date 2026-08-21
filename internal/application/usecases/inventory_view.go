package usecases

import (
	"context"

	"github.com/claudioed/wes-work-planning/internal/application/ports"
	"github.com/claudioed/wes-work-planning/internal/domain/inventoryview"
)

// InventoryView is a read-only query use case exposing the projected
// UsableInventoryObserved read model for a SKU.
type InventoryView struct {
	views ports.InventoryViewRepo
}

func NewInventoryView(views ports.InventoryViewRepo) *InventoryView {
	return &InventoryView{views: views}
}

type InventoryViewRequest struct {
	SKU string
}

func (uc *InventoryView) Execute(ctx context.Context, req InventoryViewRequest) (inventoryview.UsableInventoryObserved, error) {
	return uc.views.FindBySKU(ctx, req.SKU)
}
