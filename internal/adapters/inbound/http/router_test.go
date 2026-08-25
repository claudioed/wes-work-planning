package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	inboundhttp "github.com/claudioed/wes-work-planning/internal/adapters/inbound/http"
	"github.com/claudioed/wes-work-planning/internal/adapters/outbound/events"
	"github.com/claudioed/wes-work-planning/internal/adapters/outbound/memory"
	"github.com/claudioed/wes-work-planning/internal/application/usecases"
	"github.com/claudioed/wes-work-planning/internal/domain/laborview"
	"github.com/claudioed/wes-work-planning/internal/domain/shared"
)

func newTestRouter() http.Handler {
	charges := memory.NewChargeRepo()
	plans := memory.NewPlanRepo()
	pools := memory.NewWorkPoolRepo()
	workUnits := memory.NewWorkUnitRepo()
	laborPlanViews := memory.NewLaborPlanViewRepo()
	inventoryViews := memory.NewInventoryViewRepo()
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
		LaborPlanView:         usecases.NewLaborPlanView(laborPlanViews),
		InventoryView:         usecases.NewInventoryView(inventoryViews),
	}

	return inboundhttp.NewRouter(h, "wes-work-planning", nil)
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
	if loc := rec.Header().Get("Location"); loc != "/paths/pick-a/charge" {
		t.Fatalf("got Location %q, want /paths/pick-a/charge", loc)
	}
}

func TestPostChargeForecast_MalformedJSONReturns400(t *testing.T) {
	router := newTestRouter()
	req := httptest.NewRequest(http.MethodPost, "/paths/pick-a/charge", bytes.NewReader([]byte("{not-json")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want 400, body=%s", rec.Code, rec.Body.String())
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
	if loc := rec.Header().Get("Location"); loc != "/paths/pick-a/plan" {
		t.Fatalf("got Location %q, want /paths/pick-a/plan", loc)
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
	if loc := rec.Header().Get("Location"); loc != "/work-units/wu-1" {
		t.Fatalf("got Location %q, want /work-units/wu-1", loc)
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got, ok := resp["giftWrap"].(bool); !ok || got != false {
		t.Fatalf("got giftWrap %v, want false when omitted from the request", resp["giftWrap"])
	}
}

func TestPostWorkUnit_GiftWrapRequested_ReflectedInResponse(t *testing.T) {
	router := newTestRouter()
	body := map[string]any{
		"workUnitId": "wu-gift-wrap",
		"cpt":        time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC),
		"reference":  "order-line-1",
		"giftWrap":   true,
	}

	rec := doJSON(t, router, http.MethodPost, "/paths/pick-a/work-units", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("got status %d, want 201, body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got, ok := resp["giftWrap"].(bool); !ok || got != true {
		t.Fatalf("got giftWrap %v, want true", resp["giftWrap"])
	}
}

func TestPostWorkUnit_MalformedJSONReturns400(t *testing.T) {
	router := newTestRouter()
	req := httptest.NewRequest(http.MethodPost, "/paths/pick-a/work-units", bytes.NewReader([]byte("not json at all")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want 400, body=%s", rec.Code, rec.Body.String())
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
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("got Content-Type %q, want application/problem+json", ct)
	}
	var problem struct {
		Type     string `json:"type"`
		Title    string `json:"title"`
		Status   int    `json:"status"`
		Detail   string `json:"detail"`
		Instance string `json:"instance"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
		t.Fatalf("unmarshal problem+json body: %v", err)
	}
	if problem.Status != http.StatusConflict {
		t.Fatalf("got problem.status %d, want 409", problem.Status)
	}
	if problem.Instance != "/work-units/wu-1/complete" {
		t.Fatalf("got instance %q, want /work-units/wu-1/complete", problem.Instance)
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
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("got Content-Type %q, want application/problem+json", ct)
	}
	var problem struct {
		Type     string `json:"type"`
		Title    string `json:"title"`
		Status   int    `json:"status"`
		Detail   string `json:"detail"`
		Instance string `json:"instance"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
		t.Fatalf("unmarshal problem+json body: %v", err)
	}
	if problem.Type == "" || problem.Title == "" {
		t.Fatalf("expected non-empty type/title, got %+v", problem)
	}
	if problem.Status != http.StatusNotFound {
		t.Fatalf("got problem.status %d, want 404", problem.Status)
	}
	if problem.Detail == "" {
		t.Fatalf("expected non-empty detail, got %+v", problem)
	}
	if problem.Instance != "/paths/unknown-path/telemetry" {
		t.Fatalf("got instance %q, want /paths/unknown-path/telemetry", problem.Instance)
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

func TestGetLaborPlanView_UnknownPathReturns404(t *testing.T) {
	router := newTestRouter()
	rec := doJSON(t, router, http.MethodGet, "/paths/pick-a/labor-plan-view", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("got status %d, want 404, body=%s", rec.Code, rec.Body.String())
	}
}

func TestGetLaborPlanView_ReturnsObservedPlanAndSurfacesInRebalance(t *testing.T) {
	laborPlanViews := memory.NewLaborPlanViewRepo()
	pathId, _ := shared.NewPathId("pick-a")
	if err := laborPlanViews.Save(context.Background(), laborview.LaborPlanObserved{
		PathId: pathId, PlannedHeads: 4, PlannedRate: 90, PlannedHours: 8,
		ObservedAt: time.Date(2026, 8, 21, 9, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("seed labor plan view: %v", err)
	}

	pools := memory.NewWorkPoolRepo()
	workUnits := memory.NewWorkUnitRepo()
	publisher := events.NewLogPublisher(nil)
	clock := memory.FixedClock{At: time.Date(2026, 8, 21, 8, 0, 0, 0, time.UTC)}

	h := &inboundhttp.Handlers{
		EnqueueWorkUnit:   usecases.NewEnqueueWorkUnit(workUnits, pools, publisher, clock),
		SampleBacklog:     usecases.NewSampleBacklog(pools, publisher, clock),
		RebalanceDecision: usecases.NewRebalanceDecision(pools, publisher, clock),
		LaborPlanView:     usecases.NewLaborPlanView(laborPlanViews),
	}
	router := inboundhttp.NewRouter(h, "wes-work-planning", nil)

	enqueueBody := map[string]any{
		"workUnitId": "wu-1",
		"cpt":        time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC),
		"reference":  "order-line-1",
	}
	if rec := doJSON(t, router, http.MethodPost, "/paths/pick-a/work-units", enqueueBody); rec.Code != http.StatusCreated {
		t.Fatalf("setup: got status %d, want 201, body=%s", rec.Code, rec.Body.String())
	}

	rec := doJSON(t, router, http.MethodGet, "/paths/pick-a/labor-plan-view", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var view map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if view["plannedHeads"] != float64(4) {
		t.Fatalf("unexpected labor-plan-view body: %s", rec.Body.String())
	}

	rebalanceRec := doJSON(t, router, http.MethodGet, "/paths/pick-a/rebalance", nil)
	if rebalanceRec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200, body=%s", rebalanceRec.Code, rebalanceRec.Body.String())
	}
	var rebalance map[string]any
	if err := json.Unmarshal(rebalanceRec.Body.Bytes(), &rebalance); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	laborPlan, ok := rebalance["laborPlan"].(map[string]any)
	if !ok {
		t.Fatalf("expected rebalance response to include laborPlan, got %s", rebalanceRec.Body.String())
	}
	if laborPlan["plannedHeads"] != float64(4) {
		t.Fatalf("unexpected laborPlan in rebalance response: %v", laborPlan)
	}
}

func TestGetInventoryView_UnknownSKUReturns404(t *testing.T) {
	router := newTestRouter()
	rec := doJSON(t, router, http.MethodGet, "/inventory-view/sku-1", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("got status %d, want 404, body=%s", rec.Code, rec.Body.String())
	}
}

func TestGetInventoryView_ReturnsObservedQuantity(t *testing.T) {
	inventoryViews := memory.NewInventoryViewRepo()
	if _, err := inventoryViews.ApplyDelta(context.Background(), "sku-1", -3, time.Date(2026, 8, 21, 9, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("seed inventory view: %v", err)
	}

	h := &inboundhttp.Handlers{InventoryView: usecases.NewInventoryView(inventoryViews)}
	router := inboundhttp.NewRouter(h, "wes-work-planning", nil)

	rec := doJSON(t, router, http.MethodGet, "/inventory-view/sku-1", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var view map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if view["usableQuantity"] != float64(-3) {
		t.Fatalf("unexpected inventory-view body: %s", rec.Body.String())
	}
}
