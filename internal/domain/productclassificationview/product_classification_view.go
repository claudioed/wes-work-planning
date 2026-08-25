// Package productclassificationview holds the read-only view of a SKU's
// ProductClassification as read from inventory-storage.
//
// Unlike internal/domain/inventoryview (a persisted projection built from
// StockReserved/ReservationRevoked Kafka events), this is NOT a Kafka
// projection: inventory-storage's own outbound Kafka publisher forwards
// only StockReserved and ReservationRevoked to the broker (see its
// publisher.go doc comment and apis/asyncapi.yaml — ProductClassified is
// catalogued but explicitly not part of the published integration
// contract). There is nothing to consume. Instead this is a plain value
// returned by a synchronous outbound HTTP lookup against inventory-storage's
// GET /products/{sku}/classification, performed once at work-release time
// (see ADR-0009). The package still lives in internal/domain because it is
// a pure value type with no adapter/framework dependency, matching this
// repo's convention that domain holds every plain read-model value
// regardless of how it is populated.
package productclassificationview

// ProductClassificationView is the classification inventory-storage reports
// for a SKU at the moment it was looked up. Plain read-model value, not an
// aggregate with invariants — the classification is owned by
// inventory-storage; this service only observes and republishes derived
// hints.
//
// Known distinguishes "SKU has no registered classification" or "lookup
// unavailable" (Known=false, both treated identically — permissive/fail-open,
// see ADR-0009) from "classification confirmed, tags may still be empty in
// principle" (Known=true).
type ProductClassificationView struct {
	SKU              string
	HandlingTags     []string
	TemperatureClass string
	Known            bool
}

// HasTag reports whether the view carries tag (e.g. "Hazmat", "Fragile") —
// a small convenience so callers do not repeat a linear scan.
func (v ProductClassificationView) HasTag(tag string) bool {
	for _, t := range v.HandlingTags {
		if t == tag {
			return true
		}
	}
	return false
}
