// Package analyticsstore provides the outbound adapters that persist and
// serve the work-planning "Release Throughput & Backlog Health" read model:
// an in-memory implementation (MemoryStore) for tests and local runs, and
// Postgres implementations (a writer projection and a read-only reader) for
// deployment. All satisfy the report.ProjectionStore and/or report.ReportStore
// ports. It also owns the analytical schema migration runner and the
// consumer-side idempotency repo (ADR-0011).
package analyticsstore

import (
	"context"
	"sync"
	"time"

	"github.com/claudioed/wes-work-planning/internal/analytics/report"
)

// MemoryStore is an in-memory implementation of both report.ProjectionStore
// (write) and report.ReportStore (read), backed by maps. It is idempotent
// per eventId via a seen-set, so a duplicate delivery is a no-op. It is safe
// for concurrent use.
type MemoryStore struct {
	// Now supplies the current time for FreshnessLag; defaults to time.Now
	// when nil so lag is deterministic under test.
	Now func() time.Time

	mu   sync.Mutex
	seen map[string]struct{}
	rows map[report.RowKey]*rowAcc
	// latest is the OccurredAt of the most recently applied event, used to
	// compute FreshnessLag.
	latest time.Time
}

// rowAcc accumulates the running counters for one report row.
type rowAcc struct {
	workReleased             int
	workUnitCompleted        int
	backlogThresholdBreached int
	pathThrottled            int
	rateDeviationDetected    int
}

// NewMemoryStore constructs an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		seen: map[string]struct{}{},
		rows: map[report.RowKey]*rowAcc{},
	}
}

func hourBucket(t time.Time) time.Time {
	return t.UTC().Truncate(time.Hour)
}

// firstApply marks eventId as seen and reports whether this is the first
// time (so the caller should apply the effect) or a duplicate (skip). It
// also advances the freshness watermark. The caller must hold s.mu.
func (s *MemoryStore) firstApply(eventId string, at time.Time) bool {
	if _, dup := s.seen[eventId]; dup {
		return false
	}
	s.seen[eventId] = struct{}{}
	if at.After(s.latest) {
		s.latest = at
	}
	return true
}

func (s *MemoryStore) row(k report.RowKey) *rowAcc {
	r, ok := s.rows[k]
	if !ok {
		r = &rowAcc{}
		s.rows[k] = r
	}
	return r
}

// ApplyWorkReleased increments the row's WorkReleased counter. Idempotent on
// eventId.
func (s *MemoryStore) ApplyWorkReleased(_ context.Context, eventId, pathId string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.firstApply(eventId, at) {
		return nil
	}
	s.row(report.RowKey{PathId: pathId, HourBucket: hourBucket(at)}).workReleased++
	return nil
}

// ApplyWorkUnitCompleted increments the row's WorkUnitCompleted counter.
// Idempotent on eventId.
func (s *MemoryStore) ApplyWorkUnitCompleted(_ context.Context, eventId, pathId string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.firstApply(eventId, at) {
		return nil
	}
	s.row(report.RowKey{PathId: pathId, HourBucket: hourBucket(at)}).workUnitCompleted++
	return nil
}

// ApplyBacklogThresholdBreached increments the row's
// BacklogThresholdBreached counter. Idempotent on eventId.
func (s *MemoryStore) ApplyBacklogThresholdBreached(_ context.Context, eventId, pathId string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.firstApply(eventId, at) {
		return nil
	}
	s.row(report.RowKey{PathId: pathId, HourBucket: hourBucket(at)}).backlogThresholdBreached++
	return nil
}

// ApplyPathThrottled increments the row's PathThrottled counter. Idempotent
// on eventId.
func (s *MemoryStore) ApplyPathThrottled(_ context.Context, eventId, pathId string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.firstApply(eventId, at) {
		return nil
	}
	s.row(report.RowKey{PathId: pathId, HourBucket: hourBucket(at)}).pathThrottled++
	return nil
}

// ApplyRateDeviationDetected increments the row's RateDeviationDetected
// counter. Idempotent on eventId.
func (s *MemoryStore) ApplyRateDeviationDetected(_ context.Context, eventId, pathId string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.firstApply(eventId, at) {
		return nil
	}
	s.row(report.RowKey{PathId: pathId, HourBucket: hourBucket(at)}).rateDeviationDetected++
	return nil
}

// Query returns the rows matching q. From is inclusive, To is exclusive,
// both compared against a row's HourBucket; an empty PathId means no filter
// on that dimension.
func (s *MemoryStore) Query(_ context.Context, q report.ReportQuery) (report.ThroughputReport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := report.ThroughputReport{}
	for k, r := range s.rows {
		if k.HourBucket.Before(q.From) || !k.HourBucket.Before(q.To) {
			continue
		}
		if q.PathId != "" && k.PathId != q.PathId {
			continue
		}
		out.Rows = append(out.Rows, report.Row{
			Key:                      k,
			WorkReleased:             r.workReleased,
			WorkUnitCompleted:        r.workUnitCompleted,
			BacklogThresholdBreached: r.backlogThresholdBreached,
			PathThrottled:            r.pathThrottled,
			RateDeviationDetected:    r.rateDeviationDetected,
		})
	}
	return out, nil
}

// FreshnessLag returns how far the read model lags real time: now minus the
// OccurredAt of the most recently applied event. Zero when nothing has been
// applied yet, and never negative (a future-dated event clamps to zero).
func (s *MemoryStore) FreshnessLag(_ context.Context) (time.Duration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.latest.IsZero() {
		return 0, nil
	}
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	lag := now.Sub(s.latest)
	if lag < 0 {
		return 0, nil
	}
	return lag, nil
}

// Compile-time assertions that MemoryStore satisfies both report ports.
var (
	_ report.ProjectionStore = (*MemoryStore)(nil)
	_ report.ReportStore     = (*MemoryStore)(nil)
)
