package shared

// Rate is a service rate expressed in units per hour.
type Rate struct {
	unitsPerHour float64
}

// NewRate validates and constructs a Rate. Rate must be positive.
func NewRate(unitsPerHour float64) (Rate, error) {
	if unitsPerHour <= 0 {
		return Rate{}, ErrInvalidRate
	}
	return Rate{unitsPerHour: unitsPerHour}, nil
}

func (r Rate) UnitsPerHour() float64 {
	return r.unitsPerHour
}
