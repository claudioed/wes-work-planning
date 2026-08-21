// Package shared holds value objects, domain events, and domain errors
// shared across the WES domain: CPT, Rate, PathId, Quantity, StationCount.
package shared

import "errors"

var (
	ErrInvalidQuantity     = errors.New("quantity must be non-negative")
	ErrInvalidRate         = errors.New("rate must be positive")
	ErrInvalidStationCount = errors.New("station count must be non-negative")
	ErrInvalidPathId       = errors.New("path id must not be empty")
	ErrInvalidHours        = errors.New("hours must be positive")
)
