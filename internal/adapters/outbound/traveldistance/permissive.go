package traveldistance

import (
	"context"

	"github.com/claudioed/wes-work-planning/internal/domain/traveldistanceview"
)

// PermissiveLookup is the default ports.TravelDistanceLookup: it never
// contacts facility-layout and always reports Known=false, which
// CommitShiftPlan treats as "no travel-distance hint available, commit the
// plan with that field omitted" (fail-open). Selected via
// TRAVEL_DISTANCE_MODE (default "permissive"), so existing tests, CI and
// deployments that do not set the env var see identical behaviour to
// before this feature existed.
type PermissiveLookup struct{}

// NewPermissiveLookup constructs a PermissiveLookup.
func NewPermissiveLookup() *PermissiveLookup {
	return &PermissiveLookup{}
}

func (PermissiveLookup) GetDistance(_ context.Context, from, to string) (traveldistanceview.TravelDistanceView, error) {
	return traveldistanceview.TravelDistanceView{From: from, To: to, Known: false}, nil
}
