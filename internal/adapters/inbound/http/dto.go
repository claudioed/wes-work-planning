// Package http is the inbound REST adapter: chi router, handlers, DTOs, and
// domain-error-to-HTTP-status mapping. Domain structs never leak across this
// boundary.
package http

import "time"

// problemDetails is the RFC 7807 (https://www.rfc-editor.org/rfc/rfc7807)
// Problem Details for HTTP APIs response shape used for every error
// response, served with Content-Type: application/problem+json.
type problemDetails struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail"`
	Instance string `json:"instance,omitempty"`
}

type cptBucketDTO struct {
	CPT      time.Time `json:"cpt"`
	Quantity int       `json:"quantity"`
}

type receiveChargeForecastRequestDTO struct {
	Buckets []cptBucketDTO `json:"buckets"`
}

type chargeForecastResponseDTO struct {
	PathId        string         `json:"pathId"`
	TotalQuantity int            `json:"totalQuantity"`
	Buckets       []cptBucketDTO `json:"buckets"`
	ReceivedAt    time.Time      `json:"receivedAt"`
}

type commitShiftPlanRequestDTO struct {
	PlannedHeads      int     `json:"plannedHeads"`
	InstalledStations int     `json:"installedStations"`
	RateUnitsPerHour  float64 `json:"rateUnitsPerHour"`
	Hours             float64 `json:"hours"`
}

type shiftPlanResponseDTO struct {
	PathId            string  `json:"pathId"`
	PlannedHeads      int     `json:"plannedHeads"`
	InstalledStations int     `json:"installedStations"`
	RateUnitsPerHour  float64 `json:"rateUnitsPerHour"`
	Hours             float64 `json:"hours"`
	PlannedThroughput float64 `json:"plannedThroughput"`
}

type enqueueWorkUnitRequestDTO struct {
	WorkUnitId string    `json:"workUnitId"`
	CPT        time.Time `json:"cpt"`
	Reference  string    `json:"reference"`
	// SKU is optional: the inventory SKU this order line corresponds to,
	// if known. Threaded through so the released WorkReleased integration
	// event can carry derived hazmat/fragile hints (see ADR-0009). Absent
	// or empty is valid — not every caller knows a SKU.
	SKU string `json:"sku,omitempty"`
	// GiftWrap is optional: whether the requester asked the warehouse to
	// produce a gift package for this work unit, if known at enqueue
	// time. Threaded through so the released WorkReleased integration
	// event carries it directly (see ADR-0010) — unlike SKU, this is
	// never looked up from another service. Absent defaults to false.
	GiftWrap bool `json:"giftWrap,omitempty"`
}

type workUnitResponseDTO struct {
	Id        string    `json:"id"`
	PathId    string    `json:"pathId"`
	CPT       time.Time `json:"cpt"`
	Reference string    `json:"reference"`
	State     string    `json:"state"`
	GiftWrap  bool      `json:"giftWrap"`
	// SKU is the optional inventory SKU this work unit's order line
	// corresponds to, omitted when not known (see EnqueueWorkUnitRequest.SKU).
	SKU string `json:"sku,omitempty"`
	// ReleasedAt/CompletedAt are omitted (not null) until the work unit
	// reaches the corresponding lifecycle state.
	ReleasedAt  *time.Time `json:"releasedAt,omitempty"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
}

type backlogSnapshotResponseDTO struct {
	PathId             string `json:"pathId"`
	BacklogDepth       int    `json:"backlogDepth"`
	WIP                int    `json:"wip"`
	Mode               string `json:"mode"`
	OverAlarmThreshold bool   `json:"overAlarmThreshold"`
}

type rebalanceResponseDTO struct {
	PathId       string            `json:"pathId"`
	Action       string            `json:"action"`
	BacklogDepth int               `json:"backlogDepth"`
	WIP          int               `json:"wip"`
	LaborPlan    *laborPlanViewDTO `json:"laborPlan,omitempty"`
}

type laborPlanViewDTO struct {
	PathId       string    `json:"pathId"`
	PlannedHeads int       `json:"plannedHeads"`
	PlannedRate  float64   `json:"plannedRate"`
	PlannedHours float64   `json:"plannedHours"`
	ObservedAt   time.Time `json:"observedAt"`
}

type inventoryViewResponseDTO struct {
	SKU            string    `json:"sku"`
	UsableQuantity int       `json:"usableQuantity"`
	ObservedAt     time.Time `json:"observedAt"`
}
