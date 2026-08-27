package report_test

import (
	"context"
	"testing"
	"time"

	"github.com/claudioed/wes-work-planning/internal/analytics/report"
)

// fakeStore is an in-memory implementation of both report ports used to
// exercise report derivation from a synthetic event sequence. It is a test
// double local to this package: the production stores live in the
// analyticsstore outbound adapter.
type fakeStore struct {
	seen map[string]bool
	rows map[report.RowKey]*acc
}

// acc is the fake store's per-row accumulator, kept separate from the public
// report.Row so the running-total intermediate state never leaks into the
// read-model type.
type acc struct {
	workReleased             int
	workUnitCompleted        int
	backlogThresholdBreached int
	pathThrottled            int
	rateDeviationDetected    int
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		seen: map[string]bool{},
		rows: map[report.RowKey]*acc{},
	}
}

func (s *fakeStore) row(k report.RowKey) *acc {
	r, ok := s.rows[k]
	if !ok {
		r = &acc{}
		s.rows[k] = r
	}
	return r
}

func (s *fakeStore) dup(eventId string) bool {
	if s.seen[eventId] {
		return true
	}
	s.seen[eventId] = true
	return false
}

func hourBucket(t time.Time) time.Time {
	return t.UTC().Truncate(time.Hour)
}

func (s *fakeStore) ApplyWorkReleased(_ context.Context, eventId, pathId string, at time.Time) error {
	if s.dup(eventId) {
		return nil
	}
	s.row(report.RowKey{PathId: pathId, HourBucket: hourBucket(at)}).workReleased++
	return nil
}

func (s *fakeStore) ApplyWorkUnitCompleted(_ context.Context, eventId, pathId string, at time.Time) error {
	if s.dup(eventId) {
		return nil
	}
	s.row(report.RowKey{PathId: pathId, HourBucket: hourBucket(at)}).workUnitCompleted++
	return nil
}

func (s *fakeStore) ApplyBacklogThresholdBreached(_ context.Context, eventId, pathId string, at time.Time) error {
	if s.dup(eventId) {
		return nil
	}
	s.row(report.RowKey{PathId: pathId, HourBucket: hourBucket(at)}).backlogThresholdBreached++
	return nil
}

func (s *fakeStore) ApplyPathThrottled(_ context.Context, eventId, pathId string, at time.Time) error {
	if s.dup(eventId) {
		return nil
	}
	s.row(report.RowKey{PathId: pathId, HourBucket: hourBucket(at)}).pathThrottled++
	return nil
}

func (s *fakeStore) ApplyRateDeviationDetected(_ context.Context, eventId, pathId string, at time.Time) error {
	if s.dup(eventId) {
		return nil
	}
	s.row(report.RowKey{PathId: pathId, HourBucket: hourBucket(at)}).rateDeviationDetected++
	return nil
}

func (s *fakeStore) Query(_ context.Context, q report.ReportQuery) (report.ThroughputReport, error) {
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

func (s *fakeStore) FreshnessLag(_ context.Context) (time.Duration, error) {
	return 0, nil
}

func TestThroughputReport_DerivesFromEventSequence(t *testing.T) {
	base := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	s := newFakeStore()
	ctx := context.Background()

	// Synthetic sequence for pick-zone-a in one hour bucket:
	//  - two work units released, one completed
	//  - one backlog-threshold breach, one throttle, one rate deviation
	// Plus one unrelated event on pack-station-1.
	must(t, s.ApplyWorkReleased(ctx, "e1", "pick-zone-a", base))
	must(t, s.ApplyWorkReleased(ctx, "e2", "pick-zone-a", base.Add(time.Minute)))
	must(t, s.ApplyWorkUnitCompleted(ctx, "e3", "pick-zone-a", base.Add(2*time.Minute)))
	must(t, s.ApplyBacklogThresholdBreached(ctx, "e4", "pick-zone-a", base.Add(3*time.Minute)))
	must(t, s.ApplyPathThrottled(ctx, "e5", "pick-zone-a", base.Add(4*time.Minute)))
	must(t, s.ApplyRateDeviationDetected(ctx, "e6", "pick-zone-a", base.Add(5*time.Minute)))
	must(t, s.ApplyWorkReleased(ctx, "e7", "pack-station-1", base.Add(6*time.Minute)))

	rep, err := s.Query(ctx, report.ReportQuery{
		From:        base.Add(-time.Hour),
		To:          base.Add(2 * time.Hour),
		Granularity: report.GranularityHour,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	bucket := base.Truncate(time.Hour)
	pickRow := findRow(rep, report.RowKey{PathId: "pick-zone-a", HourBucket: bucket})
	if pickRow == nil {
		t.Fatal("no pick-zone-a row")
	}
	if pickRow.WorkReleased != 2 {
		t.Errorf("WorkReleased = %d, want 2", pickRow.WorkReleased)
	}
	if pickRow.WorkUnitCompleted != 1 {
		t.Errorf("WorkUnitCompleted = %d, want 1", pickRow.WorkUnitCompleted)
	}
	if pickRow.BacklogThresholdBreached != 1 {
		t.Errorf("BacklogThresholdBreached = %d, want 1", pickRow.BacklogThresholdBreached)
	}
	if pickRow.PathThrottled != 1 {
		t.Errorf("PathThrottled = %d, want 1", pickRow.PathThrottled)
	}
	if pickRow.RateDeviationDetected != 1 {
		t.Errorf("RateDeviationDetected = %d, want 1", pickRow.RateDeviationDetected)
	}

	packRow := findRow(rep, report.RowKey{PathId: "pack-station-1", HourBucket: bucket})
	if packRow == nil || packRow.WorkReleased != 1 {
		t.Errorf("pack-station-1 WorkReleased = %v, want 1", packRow)
	}
}

func TestThroughputReport_FiltersAndIdempotency(t *testing.T) {
	base := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	ctx := context.Background()

	tests := []struct {
		name  string
		query report.ReportQuery
		want  int // number of rows expected
	}{
		{"no filter", report.ReportQuery{From: base.Add(-time.Hour), To: base.Add(time.Hour), Granularity: report.GranularityHour}, 2},
		{"path filter", report.ReportQuery{From: base.Add(-time.Hour), To: base.Add(time.Hour), PathId: "pick-zone-a", Granularity: report.GranularityHour}, 1},
		{"window excludes all", report.ReportQuery{From: base.Add(24 * time.Hour), To: base.Add(48 * time.Hour), Granularity: report.GranularityHour}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newFakeStore()
			// Apply the same release twice with the same eventId → counts once.
			must(t, s.ApplyWorkReleased(ctx, "dup", "pick-zone-a", base))
			must(t, s.ApplyWorkReleased(ctx, "dup", "pick-zone-a", base))
			must(t, s.ApplyWorkReleased(ctx, "other", "pack-station-1", base))

			rep, err := s.Query(ctx, tt.query)
			if err != nil {
				t.Fatalf("Query: %v", err)
			}
			if len(rep.Rows) != tt.want {
				t.Errorf("rows = %d, want %d", len(rep.Rows), tt.want)
			}
			if tt.name == "no filter" {
				pick := findRow(rep, report.RowKey{PathId: "pick-zone-a", HourBucket: base.Truncate(time.Hour)})
				if pick == nil || pick.WorkReleased != 1 {
					t.Errorf("dedupe failed: pick WorkReleased = %v", pick)
				}
			}
		})
	}
}

func findRow(rep report.ThroughputReport, k report.RowKey) *report.Row {
	for i := range rep.Rows {
		if rep.Rows[i].Key == k {
			return &rep.Rows[i]
		}
	}
	return nil
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
}
