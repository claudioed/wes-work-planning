package shared

import "time"

// CPT (Critical Pull Time) is the last moment a parcel can be manifested and
// still make its truck. Work priority derives from it — earlier CPT sorts
// first.
type CPT struct {
	at time.Time
}

// NewCPT constructs a CPT from a point in time.
func NewCPT(at time.Time) CPT {
	return CPT{at: at}
}

func (c CPT) Time() time.Time {
	return c.at
}

// Before reports whether c is more urgent (earlier) than other.
func (c CPT) Before(other CPT) bool {
	return c.at.Before(other.at)
}

func (c CPT) Equals(other CPT) bool {
	return c.at.Equal(other.at)
}
