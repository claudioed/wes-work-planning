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
	uow       ports.UnitOfWork
}

func NewRebalanceDecision(pools ports.WorkPoolRepo, publisher ports.EventPublisher, clock ports.Clock) *RebalanceDecision {
	return &RebalanceDecision{pools: pools, publisher: publisher, clock: clock}
}

// WithUnitOfWork commits the Publish in one atomic scope (ADR-0014). This
// use case saves nothing, so the scope is trivial — kept uniform with every
// other publishing use case. Optional: nil publishes directly.
func (uc *RebalanceDecision) WithUnitOfWork(u ports.UnitOfWork) *RebalanceDecision {
	uc.uow = u
	return uc
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

	var event shared.DomainEvent
	switch pool.Mode() {
	case release.FlowFed:
		if pool.IsOverAlarmThreshold() {
			rec.Action = ThrottleUpstream
			event = shared.NewPathThrottled(req.PathId, now)
		}
	case release.ReleaseFed:
		if pool.WIP() >= pool.WIPLimit() && pool.BacklogDepth() > 0 {
			rec.Action = ReassignLabor
			event = shared.NewLaborReassignmentFlagged(req.PathId, now)
		}
	}
	if event != nil {
		err := atomically(ctx, uc.uow, func(ctx context.Context) error {
			return uc.publisher.Publish(ctx, event)
		})
		if err != nil {
			return RebalanceRecommendation{}, err
		}
	}

	return rec, nil
}
