// Package laborview holds the read-only projection of labor plans observed
// from Workforce Management's ShiftPlanCommitted events. This is NOT the
// same model as this service's own plan.ShiftPlan aggregate — same term,
// different bounded context.
package laborview

import (
	"time"

	"github.com/claudioed/wes-work-planning/internal/domain/shared"
)

// LaborPlanObserved is the latest labor plan observed for a path, as
// reported by Workforce Management. Plain read-model value, not an
// aggregate with invariants.
type LaborPlanObserved struct {
	PathId       shared.PathId
	PlannedHeads int
	PlannedRate  float64
	PlannedHours float64
	ObservedAt   time.Time
}
