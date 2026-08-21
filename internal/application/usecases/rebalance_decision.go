package usecases

import (
	"context"

	"github.com/claudioed/wes-work-planning/internal/application/ports"
	"github.com/claudioed/wes-work-planning/internal/domain/release"
	"github.com/claudioed/wes-work-planning/internal/domain/shared"
)

// RebalanceAction is the flow-balancing recommendation for a path.
type RebalanceAction int

const (
	// NoActionNeeded: backlog is within tolerance.
	NoActionNeeded RebalanceAction = iota
	// ThrottleUpstream: a flow-fed path's backlog is over its alarm
	// threshold — slow admission upstream of this path (drum-buffer-rope).
	ThrottleUpstream
	// ReassignLabor: a release-fed path is saturated at its WIP limit with
	// work still backlogged — flag for headcount reassignment.
	ReassignLabor
)

func (a RebalanceAction) String() string {
	switch a {
	case ThrottleUpstream:
		return "ThrottleUpstream"
	case ReassignLabor:
		return "ReassignLabor"
	default:
		return "NoActionNeeded"
	}
}

// RebalanceRecommendation is the RebalanceDecision read model.
type RebalanceRecommendation struct {
	PathId       shared.PathId
	Action       RebalanceAction
	BacklogDepth int
	WIP          int
}

// RebalanceDecision inspects a path's live buffer telemetry (backlog vs
// plan) and recommends throttling upstream release or flagging labor
// reassignment — Drum-Buffer-Rope flow balancing with CPT as the drum.
type RebalanceDecision struct {
	pools     ports.WorkPoolRepo
	publisher ports.EventPublisher
	clock     ports.Clock
}

func NewRebalanceDecision(pools ports.WorkPoolRepo, publisher ports.EventPublisher, clock ports.Clock) *RebalanceDecision {
	return &RebalanceDecision{pools: pools, publisher: publisher, clock: clock}
}

type RebalanceDecisionRequest struct {
	PathId shared.PathId
}

func (uc *RebalanceDecision) Execute(ctx context.Context, req RebalanceDecisionRequest) (RebalanceRecommendation, error) {
	pool, err := uc.pools.FindByPathId(ctx, req.PathId)
	if err != nil {
		return RebalanceRecommendation{}, err
	}

	rec := RebalanceRecommendation{
		PathId:       req.PathId,
		Action:       NoActionNeeded,
		BacklogDepth: pool.BacklogDepth(),
		WIP:          pool.WIP(),
	}

	now := uc.clock.Now()

	switch pool.Mode() {
	case release.FlowFed:
		if pool.IsOverAlarmThreshold() {
			rec.Action = ThrottleUpstream
			if err := uc.publisher.Publish(ctx, shared.NewPathThrottled(req.PathId, now)); err != nil {
				return RebalanceRecommendation{}, err
			}
		}
	case release.ReleaseFed:
		if pool.WIP() >= pool.WIPLimit() && pool.BacklogDepth() > 0 {
			rec.Action = ReassignLabor
			if err := uc.publisher.Publish(ctx, shared.NewLaborReassignmentFlagged(req.PathId, now)); err != nil {
				return RebalanceRecommendation{}, err
			}
		}
	}

	return rec, nil
}
