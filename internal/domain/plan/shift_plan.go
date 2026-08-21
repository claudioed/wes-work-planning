package plan

import "github.com/claudioed/wes-work-planning/internal/domain/shared"

// ShiftPlan is the committed split of headcount across process paths for a
// shift.
type ShiftPlan struct {
	pathPlans []PathPlan
}

// NewShiftPlan validates and constructs a ShiftPlan from its path plans.
// Requires at least one path plan; each path plan's own invariant
// (plannedHeads <= installedStations) is enforced at PathPlan construction.
func NewShiftPlan(pathPlans []PathPlan) (*ShiftPlan, error) {
	if len(pathPlans) == 0 {
		return nil, ErrNoPathPlans
	}
	copied := make([]PathPlan, len(pathPlans))
	copy(copied, pathPlans)
	return &ShiftPlan{pathPlans: copied}, nil
}

func (s *ShiftPlan) PathPlans() []PathPlan {
	out := make([]PathPlan, len(s.pathPlans))
	copy(out, s.pathPlans)
	return out
}

// PathPlan returns the plan for a given path, if present.
func (s *ShiftPlan) PathPlan(pathId shared.PathId) (PathPlan, bool) {
	for _, p := range s.pathPlans {
		if p.PathId().Equals(pathId) {
			return p, true
		}
	}
	return PathPlan{}, false
}

// TotalHours sums hours across every path plan.
func (s *ShiftPlan) TotalHours() float64 {
	total := 0.0
	for _, p := range s.pathPlans {
		total += p.Hours()
	}
	return total
}
