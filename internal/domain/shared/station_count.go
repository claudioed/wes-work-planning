package shared

// StationCount is a non-negative count of staffed or installed stations on a
// process path.
type StationCount struct {
	value int
}

// NewStationCount validates and constructs a StationCount.
func NewStationCount(value int) (StationCount, error) {
	if value < 0 {
		return StationCount{}, ErrInvalidStationCount
	}
	return StationCount{value: value}, nil
}

func (s StationCount) Value() int {
	return s.value
}

func (s StationCount) LessThan(other StationCount) bool {
	return s.value < other.value
}

func (s StationCount) GreaterThan(other StationCount) bool {
	return s.value > other.value
}
