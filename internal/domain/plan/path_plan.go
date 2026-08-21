package plan

import "github.com/claudioed/wes-work-planning/internal/domain/shared"

// PathPlan is the committed rate x heads x hours split for one process path.
// Invariant: plannedHeads must not exceed installedStations.
type PathPlan struct {
	pathId            shared.PathId
	plannedHeads      shared.StationCount
	installedStations shared.StationCount
	rate              shared.Rate
	hours             float64
}

// NewPathPlan validates and constructs a PathPlan. Enforces
// plannedHeads <= installedStations.
func NewPathPlan(pathId shared.PathId, plannedHeads, installedStations shared.StationCount, rate shared.Rate, hours float64) (PathPlan, error) {
	if plannedHeads.GreaterThan(installedStations) {
		return PathPlan{}, ErrHeadsExceedStations
	}
	if hours <= 0 {
		return PathPlan{}, shared.ErrInvalidHours
	}
	return PathPlan{
		pathId:            pathId,
		plannedHeads:      plannedHeads,
		installedStations: installedStations,
		rate:              rate,
		hours:             hours,
	}, nil
}

func (p PathPlan) PathId() shared.PathId                  { return p.pathId }
func (p PathPlan) PlannedHeads() shared.StationCount      { return p.plannedHeads }
func (p PathPlan) InstalledStations() shared.StationCount { return p.installedStations }
func (p PathPlan) Rate() shared.Rate                      { return p.rate }
func (p PathPlan) Hours() float64                         { return p.hours }

// PlannedThroughput is rate x heads x hours for this path.
func (p PathPlan) PlannedThroughput() float64 {
	return p.rate.UnitsPerHour() * float64(p.plannedHeads.Value()) * p.hours
}
