package http_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	inboundhttp "github.com/claudioed/wes-work-planning/internal/adapters/inbound/http"
	"github.com/claudioed/wes-work-planning/internal/adapters/outbound/events"
	"github.com/claudioed/wes-work-planning/internal/adapters/outbound/memory"
	"github.com/claudioed/wes-work-planning/internal/application/usecases"
)

func newTestRouter() http.Handler {
	charges := memory.NewChargeRepo()
	plans := memory.NewPlanRepo()
	pools := memory.NewWorkPoolRepo()
	workUnits := memory.NewWorkUnitRepo()
	publisher := events.NewLogPublisher(nil)
	clock := memory.FixedClock{At: time.Date(2026, 8, 21, 8, 0, 0, 0, time.UTC)}

	h := &inboundhttp.Handlers{
		ReceiveChargeForecast: usecases.NewReceiveChargeForecast(charges, publisher, clock),
		CommitShiftPlan:       usecases.NewCommitShiftPlan(plans, publisher, clock),
		EnqueueWorkUnit:       usecases.NewEnqueueWorkUnit(workUnits, pools, publisher, clock),
		ReleaseNextWork:       usecases.NewReleaseNextWork(pools, workUnits, publisher, clock),
		RecordCompletion:      usecases.NewRecordCompletion(workUnits, publisher, clock),
		SampleBacklog:         usecases.NewSampleBacklog(pools, publisher, clock),
		RebalanceDecision:     usecases.NewRebalanceDecision(pools, publisher, clock),
	}

	return inboundhttp.NewRouter(h)
}

func doJSON(t *testing.T, router http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}

	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestHealthz(t *testing.T) {
	router := newTestRouter()
	rec := doJSON(t, router, http.MethodGet, "/healthz", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200", rec.Code)
	}
}

func TestPostChargeForecast(t *testing.T) {
	router := newTestRouter()
	body := map[string]any{
		"buckets": []map[string]any{
			{"cpt": time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC), "quantity": 100},
		},
	}

	rec := doJSON(t, router, http.MethodPost, "/paths/pick-a/charge", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("got status %d, want 201, body=%s", rec.Code, rec.Body.String())
	}
}

func TestPostChargeForecast_InvalidQuantityReturns400(t *testing.T) {
	router := newTestRouter()
	body := map[string]any{
		"buckets": []map[string]any{
			{"cpt": time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC), "quantity": -5},
		},
	}

	rec := doJSON(t, router, http.MethodPost, "/paths/pick-a/charge", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want 400, body=%s", rec.Code, rec.Body.String())
	}
}

func TestPostShiftPlan(t *testing.T) {
	router := newTestRouter()
	body := map[string]any{
		"plannedHeads":      3,
		"installedStations": 5,
		"rateUnitsPerHour":  50,
		"hours":             8,
	}

	rec := doJSON(t, router, http.MethodPost, "/paths/pick-a/plan", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("got status %d, want 201, body=%s", rec.Code, rec.Body.String())
	}
}

func TestPostShiftPlan_HeadsExceedStationsReturns400(t *testing.T) {
	router := newTestRouter()
	body := map[string]any{
		"plannedHeads":      6,
		"installedStations": 5,
		"rateUnitsPerHour":  50,
		"hours":             8,
	}

	rec := doJSON(t, router, http.MethodPost, "/paths/pick-a/plan", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want 400, body=%s", rec.Code, rec.Body.String())
	}
}

func TestPostWorkUnit(t *testing.T) {
	router := newTestRouter()
	body := map[string]any{
		"workUnitId": "wu-1",
		"cpt":        time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC),
		"reference":  "order-line-1",
	}

	rec := doJSON(t, router, http.MethodPost, "/paths/pick-a/work-units", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("got status %d, want 201, body=%s", rec.Code, rec.Body.String())
	}
}

func TestPostRelease(t *testing.T) {
	router := newTestRouter()
	enqueueBody := map[string]any{
		"workUnitId": "wu-1",
		"cpt":        time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC),
		"reference":  "order-line-1",
	}
	if rec := doJSON(t, router, http.MethodPost, "/paths/pick-a/work-units", enqueueBody); rec.Code != http.StatusCreated {
		t.Fatalf("setup: got status %d, want 201, body=%s", rec.Code, rec.Body.String())
	}

	rec := doJSON(t, router, http.MethodPost, "/paths/pick-a/release", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
}

func TestPostRelease_EmptyPoolReturns409(t *testing.T) {
	router := newTestRouter()
	enqueueBody := map[string]any{
		"workUnitId": "wu-1",
		"cpt":        time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC),
		"reference":  "order-line-1",
	}
	if rec := doJSON(t, router, http.MethodPost, "/paths/pick-a/work-units", enqueueBody); rec.Code != http.StatusCreated {
		t.Fatalf("setup: got status %d, want 201, body=%s", rec.Code, rec.Body.String())
	}
	if rec := doJSON(t, router, http.MethodPost, "/paths/pick-a/release", nil); rec.Code != http.StatusOK {
		t.Fatalf("setup: got status %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	rec := doJSON(t, router, http.MethodPost, "/paths/pick-a/release", nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("got status %d, want 409, body=%s", rec.Code, rec.Body.String())
	}
}

func TestPostComplete(t *testing.T) {
	router := newTestRouter()
	enqueueBody := map[string]any{
		"workUnitId": "wu-1",
		"cpt":        time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC),
		"reference":  "order-line-1",
	}
	if rec := doJSON(t, router, http.MethodPost, "/paths/pick-a/work-units", enqueueBody); rec.Code != http.StatusCreated {
		t.Fatalf("setup: got status %d, want 201, body=%s", rec.Code, rec.Body.String())
	}
	if rec := doJSON(t, router, http.MethodPost, "/paths/pick-a/release", nil); rec.Code != http.StatusOK {
		t.Fatalf("setup: got status %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	rec := doJSON(t, router, http.MethodPost, "/work-units/wu-1/complete", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
}

func TestPostComplete_DoubleCompleteReturns409(t *testing.T) {
	router := newTestRouter()
	enqueueBody := map[string]any{
		"workUnitId": "wu-1",
		"cpt":        time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC),
		"reference":  "order-line-1",
	}
	doJSON(t, router, http.MethodPost, "/paths/pick-a/work-units", enqueueBody)
	doJSON(t, router, http.MethodPost, "/paths/pick-a/release", nil)
	doJSON(t, router, http.MethodPost, "/work-units/wu-1/complete", nil)

	rec := doJSON(t, router, http.MethodPost, "/work-units/wu-1/complete", nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("got status %d, want 409, body=%s", rec.Code, rec.Body.String())
	}
}

func TestGetTelemetry(t *testing.T) {
	router := newTestRouter()
	enqueueBody := map[string]any{
		"workUnitId": "wu-1",
		"cpt":        time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC),
		"reference":  "order-line-1",
	}
	doJSON(t, router, http.MethodPost, "/paths/pick-a/work-units", enqueueBody)

	rec := doJSON(t, router, http.MethodGet, "/paths/pick-a/telemetry", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
}

func TestGetTelemetry_UnknownPathReturns404(t *testing.T) {
	router := newTestRouter()
	rec := doJSON(t, router, http.MethodGet, "/paths/unknown-path/telemetry", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("got status %d, want 404, body=%s", rec.Code, rec.Body.String())
	}
}

func TestGetRebalance(t *testing.T) {
	router := newTestRouter()
	enqueueBody := map[string]any{
		"workUnitId": "wu-1",
		"cpt":        time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC),
		"reference":  "order-line-1",
	}
	doJSON(t, router, http.MethodPost, "/paths/pick-a/work-units", enqueueBody)

	rec := doJSON(t, router, http.MethodGet, "/paths/pick-a/rebalance", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
}

func TestGetRebalance_UnknownPathReturns404(t *testing.T) {
	router := newTestRouter()
	rec := doJSON(t, router, http.MethodGet, "/paths/unknown-path/rebalance", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("got status %d, want 404, body=%s", rec.Code, rec.Body.String())
	}
}
