// Package inventoryview holds the read-only projection of usable inventory
// observed from Inventory's StockReserved/ReservationRevoked events. Keyed
// by SKU — inventory reservations are SKU-scoped, not path-scoped.
package inventoryview

import "time"

// UsableInventoryObserved is the latest observed usable quantity for a SKU.
// Plain read-model value, not an aggregate with invariants.
type UsableInventoryObserved struct {
	SKU            string
	UsableQuantity int
	ObservedAt     time.Time
}
