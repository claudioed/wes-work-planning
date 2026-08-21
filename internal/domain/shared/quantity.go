package shared

// Quantity is a non-negative count of units of volume/work.
type Quantity struct {
	value int
}

// NewQuantity validates and constructs a Quantity. Must be non-negative.
func NewQuantity(value int) (Quantity, error) {
	if value < 0 {
		return Quantity{}, ErrInvalidQuantity
	}
	return Quantity{value: value}, nil
}

func (q Quantity) Value() int {
	return q.value
}

func (q Quantity) Add(other Quantity) Quantity {
	return Quantity{value: q.value + other.value}
}

func (q Quantity) Subtract(other Quantity) (Quantity, error) {
	return NewQuantity(q.value - other.value)
}
