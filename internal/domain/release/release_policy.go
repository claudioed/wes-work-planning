package release

// ReleasePolicy is the domain service that decides release admission: it is
// continuous, priority-ordered admission of work into a pool (waveless),
// applying the pool's own invariants (WIP limit on release-fed pools).
type ReleasePolicy struct{}

// NewReleasePolicy constructs the default release policy.
func NewReleasePolicy() ReleasePolicy {
	return ReleasePolicy{}
}

// Apply releases the next highest-priority unit from the pool, per the
// pool's own release rules (priority order, at-most-once, WIP limit).
func (ReleasePolicy) Apply(pool *WorkPool) (string, error) {
	return pool.ReleaseNext()
}
