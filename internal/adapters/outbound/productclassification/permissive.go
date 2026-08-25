package productclassification

import (
	"context"

	"github.com/claudioed/wes-work-planning/internal/domain/productclassificationview"
)

// PermissiveLookup is the default ports.ProductClassificationLookup: it
// never contacts inventory-storage and always reports Known=false, which
// the outbound WorkReleased publisher treats as "no derived hazmat/fragile
// hint available, publish the event with those fields omitted/false"
// (fail-open). Selected via PRODUCT_CLASSIFICATION_MODE (default
// "permissive"), so existing tests, CI and deployments that do not set the
// env var see identical behaviour to before this feature existed.
type PermissiveLookup struct{}

// NewPermissiveLookup constructs a PermissiveLookup.
func NewPermissiveLookup() *PermissiveLookup {
	return &PermissiveLookup{}
}

func (PermissiveLookup) GetClassification(_ context.Context, sku string) (productclassificationview.ProductClassificationView, error) {
	return productclassificationview.ProductClassificationView{SKU: sku, Known: false}, nil
}
