// Package charge holds the ChargeForecast aggregate: the volume that must
// clear a process path, bucketed by CPT.
package charge

import "errors"

var (
	ErrNoBuckets  = errors.New("charge forecast requires at least one CPT bucket")
	ErrUnknownCPT = errors.New("no bucket exists for the given CPT")
)
