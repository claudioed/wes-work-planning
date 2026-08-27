package analyticsstore_test

import (
	"context"
	"testing"
	"time"

	"github.com/claudioed/wes-work-planning/internal/adapters/outbound/analyticsstore"
	"github.com/claudioed/wes-work-planning/internal/analytics/report"
)

func TestMemoryStore_CountersAndIdempotency(t *testing.T) {
	base := time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC)
	ctx := context.Background()
	s := analyticsstore.NewMemoryStore()

	apply := func() {
		// Same event ids each time: a duplicate delivery must count once.
		if err := s.ApplyWorkReleased(ctx, "wr-1", "pick-zone-a", base); err != nil {
			t.Fatalf("work released: %v", err)
		}
		if err := s.ApplyWorkReleased(ctx, "wr-2", "pick-zone-a", base.Add(time.Minute)); err != nil {
			t.Fatalf("work released: %v", err)
		}
		if err := s.ApplyWorkUnitCompleted(ctx, "wc-1", "pick-zone-a", base.Add(2*time.Minute)); err != nil {
			t.Fatalf("work completed: %v", err)
		}
		if err := s.ApplyBacklogThresholdBreached(ctx, "bt-1", "pick-zone-a", base.Add(3*time.Minute)); err != nil {
			t.Fatalf("backlog breach: %v", err)
		}
		if err := s.ApplyPathThrottled(ctx, "pt-1", "pick-zone-a", base.Add(4*time.Minute)); err != nil {
			t.Fatalf("path throttled: %v", err)
		}
		if err := s.ApplyRateDeviationDetected(ctx, "rd-1", "pick-zone-a", base.Add(5*time.Minute)); err != nil {
			t.Fatalf("rate deviation: %v", err)
		}
	}

	apply()
	apply()

	rep, err := s.Query(ctx, report.ReportQuery{
		From:        base.Add(-time.Hour),
		To:          base.Add(time.Hour),
		Granularity: report.GranularityHour,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(rep.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rep.Rows))
	}
	row := rep.Rows[0]
	if row.WorkReleased != 2 {
		t.Errorf("WorkReleased = %d, want 2 (idempotent)", row.WorkReleased)
	}
	if row.WorkUnitCompleted != 1 {
		t.Errorf("WorkUnitCompleted = %d, want 1", row.WorkUnitCompleted)
	}
	if row.BacklogThresholdBreached != 1 {
		t.Errorf("BacklogThresholdBreached = %d, want 1", row.BacklogThresholdBreached)
	}
	if row.PathThrottled != 1 {
		t.Errorf("PathThrottled = %d, want 1", row.PathThrottled)
	}
	if row.RateDeviationDetected != 1 {
		t.Errorf("RateDeviationDetected = %d, want 1", row.RateDeviationDetected)
	}
}

func TestMemoryStore_PathFilterAndWindow(t *testing.T) {
	base := time.Date(2026, 4, 1, 9, 30, 0, 0, time.UTC)
	ctx := context.Background()
	s := analyticsstore.NewMemoryStore()

	if err := s.ApplyWorkReleased(ctx, "a", "pick-zone-a", base); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if err := s.ApplyWorkReleased(ctx, "b", "pack-station-1", base); err != nil {
		t.Fatalf("apply: %v", err)
	}

	tests := []struct {
		name  string
		query report.ReportQuery
		want  int
	}{
		{"no filter", report.ReportQuery{From: base.Add(-time.Hour), To: base.Add(time.Hour)}, 2},
		{"path filter", report.ReportQuery{From: base.Add(-time.Hour), To: base.Add(time.Hour), PathId: "pick-zone-a"}, 1},
		{"window excludes", report.ReportQuery{From: base.Add(24 * time.Hour), To: base.Add(48 * time.Hour)}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rep, err := s.Query(ctx, tt.query)
			if err != nil {
				t.Fatalf("Query: %v", err)
			}
			if len(rep.Rows) != tt.want {
				t.Errorf("rows = %d, want %d", len(rep.Rows), tt.want)
			}
		})
	}
}

func TestMemoryStore_FreshnessLag(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	s := analyticsstore.NewMemoryStore()
	s.Now = func() time.Time { return now }

	// No events yet: lag is zero.
	lag, err := s.FreshnessLag(ctx)
	if err != nil {
		t.Fatalf("FreshnessLag: %v", err)
	}
	if lag != 0 {
		t.Errorf("empty lag = %v, want 0", lag)
	}

	// An event 10 minutes old makes the lag 10 minutes.
	if err := s.ApplyWorkReleased(ctx, "c", "pick-zone-a", now.Add(-10*time.Minute)); err != nil {
		t.Fatalf("apply: %v", err)
	}
	lag, err = s.FreshnessLag(ctx)
	if err != nil {
		t.Fatalf("FreshnessLag: %v", err)
	}
	if lag != 10*time.Minute {
		t.Errorf("lag = %v, want 10m", lag)
	}
}

// Compile-time assertions that MemoryStore satisfies both ports.
var (
	_ report.ProjectionStore = (*analyticsstore.MemoryStore)(nil)
	_ report.ReportStore     = (*analyticsstore.MemoryStore)(nil)
)
