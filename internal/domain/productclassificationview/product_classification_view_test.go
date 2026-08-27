package productclassificationview_test

import (
	"testing"

	"github.com/claudioed/wes-work-planning/internal/domain/productclassificationview"
)

func TestProductClassificationView_HasTag(t *testing.T) {
	view := productclassificationview.ProductClassificationView{
		SKU:          "sku-1",
		HandlingTags: []string{"Hazmat", "Fragile"},
		Known:        true,
	}

	if !view.HasTag("Hazmat") {
		t.Fatalf("expected HasTag(Hazmat) to be true")
	}
	if !view.HasTag("Fragile") {
		t.Fatalf("expected HasTag(Fragile) to be true")
	}
	if view.HasTag("Oversized") {
		t.Fatalf("expected HasTag(Oversized) to be false")
	}
}

func TestProductClassificationView_HasTag_EmptyTags(t *testing.T) {
	view := productclassificationview.ProductClassificationView{SKU: "sku-1", Known: false}

	if view.HasTag("Hazmat") {
		t.Fatalf("expected HasTag on an unknown view to be false")
	}
}
