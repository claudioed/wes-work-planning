// Package plan holds the ShiftPlan and PathPlan aggregates: the committed
// split of headcount across process paths.
package plan

import "errors"

var (
	ErrHeadsExceedStations = errors.New("planned heads exceeds installed stations")
	ErrNoPathPlans         = errors.New("shift plan requires at least one path plan")
)
