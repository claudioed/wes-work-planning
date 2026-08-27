package http_test

import (
	"context"
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/claudioed/wes-work-planning/internal/adapters/inbound/http"
	"github.com/claudioed/wes-work-planning/internal/analytics/report"
)

// fakeReportStore is a test double for report.ReportStore.
type fakeReportStore struct {
	report    report.ThroughputReport
	lag       time.Duration
	queryErr  error
	freshErr  error
	lastQuery report.ReportQuery
}

func (f *fakeReportStore) Query(_ context.Context, q report.ReportQuery) (report.ThroughputReport, error) {
	f.lastQuery = q
	return f.report, f.queryErr
}

func (f *fakeReportStore) FreshnessLag(_ context.Context) (time.Duration, error) {
	return f.lag, f.freshErr
}

func newReportsServer(store report.ReportStore) stdhttp.Handler {
	return http.NewReportsRouter(&http.ReportsHandlers{Store: store}, "wes-reports-test", nil)
}

func TestReportsThroughput_OK(t *testing.T) {
	bucket := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	store := &fakeReportStore{
		report: report.ThroughputReport{Rows: []report.Row{
			{
				Key:                      report.RowKey{PathId: "pick-zone-a", HourBucket: bucket},
				WorkReleased:             5,
				WorkUnitCompleted:        4,
				BacklogThresholdBreached: 1,
				PathThrottled:            2,
				RateDeviationDetected:    0,
			},
		}},
	}
	srv := newReportsServer(store)

	req := httptest.NewRequest(stdhttp.MethodGet,
		"/reports/throughput?from=2026-06-01T00:00:00Z&to=2026-06-02T00:00:00Z&pathId=pick-zone-a&granularity=hour", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	if store.lastQuery.PathId != "pick-zone-a" {
		t.Errorf("filter not forwarded: %+v", store.lastQuery)
	}
	if store.lastQuery.Granularity != report.GranularityHour {
		t.Errorf("granularity = %q, want hour", store.lastQuery.Granularity)
	}

	var body struct {
		Rows []struct {
			PathId                   string `json:"pathId"`
			HourBucket               string `json:"hourBucket"`
			WorkReleased             int    `json:"workReleased"`
			WorkUnitCompleted        int    `json:"workUnitCompleted"`
			BacklogThresholdBreached int    `json:"backlogThresholdBreached"`
			PathThrottled            int    `json:"pathThrottled"`
			RateDeviationDetected    int    `json:"rateDeviationDetected"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(body.Rows))
	}
	row := body.Rows[0]
	if row.PathId != "pick-zone-a" || row.WorkReleased != 5 || row.WorkUnitCompleted != 4 ||
		row.BacklogThresholdBreached != 1 || row.PathThrottled != 2 {
		t.Errorf("row = %+v", row)
	}
	if row.HourBucket != "2026-06-01T10:00:00Z" {
		t.Errorf("hourBucket = %q", row.HourBucket)
	}
}

func TestReportsThroughput_MissingFromTo(t *testing.T) {
	srv := newReportsServer(&fakeReportStore{})
	tests := []struct {
		name string
		url  string
	}{
		{"no from", "/reports/throughput?to=2026-06-02T00:00:00Z"},
		{"no to", "/reports/throughput?from=2026-06-01T00:00:00Z"},
		{"bad from", "/reports/throughput?from=nope&to=2026-06-02T00:00:00Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, httptest.NewRequest(stdhttp.MethodGet, tt.url, nil))
			if rec.Code != stdhttp.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
				t.Errorf("content-type = %q, want application/problem+json", ct)
			}
		})
	}
}

func TestReportsThroughput_DefaultGranularity(t *testing.T) {
	store := &fakeReportStore{}
	srv := newReportsServer(store)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(stdhttp.MethodGet,
		"/reports/throughput?from=2026-06-01T00:00:00Z&to=2026-06-02T00:00:00Z", nil))
	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if store.lastQuery.Granularity != report.GranularityHour {
		t.Errorf("default granularity = %q, want hour", store.lastQuery.Granularity)
	}
}

func TestReportsFreshness_OK(t *testing.T) {
	store := &fakeReportStore{lag: 90 * time.Second}
	srv := newReportsServer(store)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(stdhttp.MethodGet, "/reports/throughput/freshness", nil))
	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		LagSeconds float64 `json:"lagSeconds"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.LagSeconds != 90 {
		t.Errorf("lagSeconds = %v, want 90", body.LagSeconds)
	}
}

func TestReportsHealthz(t *testing.T) {
	srv := newReportsServer(&fakeReportStore{})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(stdhttp.MethodGet, "/healthz", nil))
	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}
