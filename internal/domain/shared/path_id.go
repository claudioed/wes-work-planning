package shared

// PathId identifies a process path — a named station owning a queue
// (unit-in → unit-out), e.g. "pick-zone-a" or "pack-station-3".
type PathId struct {
	value string
}

// NewPathId validates and constructs a PathId.
func NewPathId(value string) (PathId, error) {
	if value == "" {
		return PathId{}, ErrInvalidPathId
	}
	return PathId{value: value}, nil
}

func (p PathId) String() string {
	return p.value
}

func (p PathId) Equals(other PathId) bool {
	return p.value == other.value
}
