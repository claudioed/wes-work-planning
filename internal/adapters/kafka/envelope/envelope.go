// Package envelope defines the integration-event wire format shared by
// every warehouse-systems service: a fixed envelope with an event-type-
// specific JSON payload. Used by both the outbound and inbound Kafka
// adapters so they agree on the wire format without depending on each
// other.
package envelope

import (
	"encoding/json"
	"time"
)

// Envelope is the identical outer shape published/consumed across all
// warehouse-systems services.
type Envelope struct {
	EventId    string          `json:"event_id"`
	EventType  string          `json:"event_type"`
	OccurredAt time.Time       `json:"occurred_at"`
	Source     string          `json:"source"`
	Data       json.RawMessage `json:"data"`
}

// Source identifies this service in the "source" field of every envelope
// it publishes.
const Source = "wes-work-planning"

// Topics this service publishes to / consumes from.
const (
	TopicWorkPlanningEvents = "warehouse.work-planning.events"
	TopicWorkforceEvents    = "warehouse.workforce.events"
	TopicInventoryEvents    = "warehouse.inventory.events"
	TopicFulfillmentEvents  = "warehouse.fulfillment.events"
)

// Event types this service consumes.
const (
	EventTypeShiftPlanCommitted = "ShiftPlanCommitted"
	EventTypeStockReserved      = "StockReserved"
	EventTypeReservationRevoked = "ReservationRevoked"
	EventTypeTaskCompleted      = "TaskCompleted"
)
