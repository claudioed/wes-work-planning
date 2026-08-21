package memory

import "time"

// SystemClock implements ports.Clock using the real wall clock.
type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now() }

// FixedClock implements ports.Clock returning a fixed instant — useful for
// deterministic tests.
type FixedClock struct {
	At time.Time
}

func (c FixedClock) Now() time.Time { return c.At }
