package charge

import (
	"time"

	"github.com/claudioed/wes-work-planning/internal/domain/shared"
)

// CPTBucket is the volume due by a specific CPT.
type CPTBucket struct {
	CPT      shared.CPT
	Quantity shared.Quantity
}

// ChargeForecast is the volume that must clear a process path this shift,
// bucketed by CPT. It is not "total due today" — the CPT buckets are what
// drive release priority downstream.
type ChargeForecast struct {
	pathId     shared.PathId
	buckets    []CPTBucket
	receivedAt time.Time
}

// NewChargeForecast constructs a ChargeForecast for a path from its CPT
// buckets. Requires at least one bucket.
func NewChargeForecast(pathId shared.PathId, buckets []CPTBucket, receivedAt time.Time) (*ChargeForecast, error) {
	if len(buckets) == 0 {
		return nil, ErrNoBuckets
	}
	copied := make([]CPTBucket, len(buckets))
	copy(copied, buckets)
	return &ChargeForecast{pathId: pathId, buckets: copied, receivedAt: receivedAt}, nil
}

func (f *ChargeForecast) PathId() shared.PathId {
	return f.pathId
}

func (f *ChargeForecast) Buckets() []CPTBucket {
	out := make([]CPTBucket, len(f.buckets))
	copy(out, f.buckets)
	return out
}

// TotalQuantity sums volume across every CPT bucket.
func (f *ChargeForecast) TotalQuantity() shared.Quantity {
	total, _ := shared.NewQuantity(0)
	for _, b := range f.buckets {
		total = total.Add(b.Quantity)
	}
	return total
}

// QuantityForCPT returns the quantity due at an exact CPT, or ErrUnknownCPT.
func (f *ChargeForecast) QuantityForCPT(cpt shared.CPT) (shared.Quantity, error) {
	for _, b := range f.buckets {
		if b.CPT.Equals(cpt) {
			return b.Quantity, nil
		}
	}
	return shared.Quantity{}, ErrUnknownCPT
}

func (f *ChargeForecast) ReceivedAt() time.Time {
	return f.receivedAt
}
