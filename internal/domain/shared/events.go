package shared

import "time"

// DomainEvent is a past-tense fact published by an aggregate.
type DomainEvent interface {
	EventName() string
	OccurredAt() time.Time
}

type baseEvent struct {
	name string
	at   time.Time
}

func (b baseEvent) EventName() string     { return b.name }
func (b baseEvent) OccurredAt() time.Time { return b.at }

// ChargeForecastReceived is raised when a shift's charge forecast is recorded
// for a process path.
type ChargeForecastReceived struct {
	baseEvent
	PathId PathId
}

func NewChargeForecastReceived(pathId PathId, at time.Time) ChargeForecastReceived {
	return ChargeForecastReceived{baseEvent: baseEvent{name: "ChargeForecastReceived", at: at}, PathId: pathId}
}

// ShiftPlanCommitted is raised when a shift plan's headcount/rate/hours split
// is committed for a process path.
type ShiftPlanCommitted struct {
	baseEvent
	PathId PathId
}

func NewShiftPlanCommitted(pathId PathId, at time.Time) ShiftPlanCommitted {
	return ShiftPlanCommitted{baseEvent: baseEvent{name: "ShiftPlanCommitted", at: at}, PathId: pathId}
}

// WorkUnitCreated is raised when a new work unit is enqueued into a path's
// work pool.
type WorkUnitCreated struct {
	baseEvent
	WorkUnitId string
	PathId     PathId
}

func NewWorkUnitCreated(workUnitId string, pathId PathId, at time.Time) WorkUnitCreated {
	return WorkUnitCreated{baseEvent: baseEvent{name: "WorkUnitCreated", at: at}, WorkUnitId: workUnitId, PathId: pathId}
}

// WorkReleased is raised when the release policy admits a work unit from the
// pool into active work.
type WorkReleased struct {
	baseEvent
	WorkUnitId string
	PathId     PathId
}

func NewWorkReleased(workUnitId string, pathId PathId, at time.Time) WorkReleased {
	return WorkReleased{baseEvent: baseEvent{name: "WorkReleased", at: at}, WorkUnitId: workUnitId, PathId: pathId}
}

// BacklogThresholdBreached is raised when a path's backlog depth crosses its
// alarm threshold.
type BacklogThresholdBreached struct {
	baseEvent
	PathId PathId
}

func NewBacklogThresholdBreached(pathId PathId, at time.Time) BacklogThresholdBreached {
	return BacklogThresholdBreached{baseEvent: baseEvent{name: "BacklogThresholdBreached", at: at}, PathId: pathId}
}

// RateDeviationDetected is raised when a path's actual throughput deviates
// materially from plan.
type RateDeviationDetected struct {
	baseEvent
	PathId PathId
}

func NewRateDeviationDetected(pathId PathId, at time.Time) RateDeviationDetected {
	return RateDeviationDetected{baseEvent: baseEvent{name: "RateDeviationDetected", at: at}, PathId: pathId}
}

// PathThrottled is raised when flow balancing decides to throttle upstream
// release into a path.
type PathThrottled struct {
	baseEvent
	PathId PathId
}

func NewPathThrottled(pathId PathId, at time.Time) PathThrottled {
	return PathThrottled{baseEvent: baseEvent{name: "PathThrottled", at: at}, PathId: pathId}
}

// LaborReassignmentFlagged is raised when flow balancing recommends moving
// labor to relieve a path.
type LaborReassignmentFlagged struct {
	baseEvent
	PathId PathId
}

func NewLaborReassignmentFlagged(pathId PathId, at time.Time) LaborReassignmentFlagged {
	return LaborReassignmentFlagged{baseEvent: baseEvent{name: "LaborReassignmentFlagged", at: at}, PathId: pathId}
}

// WorkUnitCompleted is raised when a released work unit finishes.
type WorkUnitCompleted struct {
	baseEvent
	WorkUnitId string
	PathId     PathId
}

func NewWorkUnitCompleted(workUnitId string, pathId PathId, at time.Time) WorkUnitCompleted {
	return WorkUnitCompleted{baseEvent: baseEvent{name: "WorkUnitCompleted", at: at}, WorkUnitId: workUnitId, PathId: pathId}
}
