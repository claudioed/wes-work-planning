package http_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	inboundhttp "github.com/claudioed/wes-work-planning/internal/adapters/inbound/http"
	"github.com/claudioed/wes-work-planning/internal/adapters/outbound/events"
	"github.com/claudioed/wes-work-planning/internal/adapters/outbound/memory"
	"github.com/claudioed/wes-work-planning/internal/application/usecases"
	"github.com/claudioed/wes-work-planning/internal/domain/traveldistanceview"
)

// stubTravelDistanceLookup is a fixed-response ports.TravelDistanceLookup
// for HTTP-layer tests.
type stubTravelDistanceLookup struct {
	view traveldistanceview.TravelDistanceView
}

func (s stubTravelDistanceLookup) GetDistance(_ context.Context, from, to string) (traveldistanceview.TravelDistanceView, error) {
	v := s.view
	v.From, v.To = from, to
	return v, nil
}

func newTestRouterWithTravelDistance(lookup stubTravelDistanceLookup) http.Handler {
	plans := memory.NewPlanRepo()
	publisher := events.NewLogPublisher(nil)
	clock := memory.FixedClock{At: time.Date(2026, 8, 21, 8, 0, 0, 0, time.UTC)}

	h := newTestHandlers()
	h.CommitShiftPlan = usecases.NewCommitShiftPlan(plans, publisher, clock).WithTravelDistanceLookup(lookup)
	return inboundhttp.NewRouter(h, "wes-work-planning", nil)
}

func TestPostShiftPlan_WithTravelDistance_KnownEnrichesResponse(t *testing.T) {
	router := newTestRouterWithTravelDistance(stubTravelDistanceLookup{
		view: traveldistanceview.TravelDistanceView{MetresM: 4.4, Estimated: false, Known: true},
	})
	body := map[string]any{
		"plannedHeads":      3,
		"installedStations": 5,
		"rateUnitsPerHour":  50,
		"hours":             8,
		"fromLocationCode":  "WH1-STOR-AMB-A07-01-01-A",
		"toLocationCode":    "WH1-STOR-AMB-A09-03-01-A",
	}

	rec := doJSON(t, router, http.MethodPost, "/paths/pick-a/plan", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("got status %d, want 201, body=%s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	metres, ok := resp["travelDistanceM"]
	if !ok {
		t.Fatalf("expected travelDistanceM in response, got %s", rec.Body.String())
	}
	if metres != 4.4 {
		t.Fatalf("got travelDistanceM %v, want 4.4", metres)
	}
	if estimated, ok := resp["travelDistanceEstimated"]; !ok || estimated != false {
		t.Fatalf("got travelDistanceEstimated %v, want false", resp["travelDistanceEstimated"])
	}
}

func TestPostShiftPlan_WithoutLocationCodes_OmitsTravelDistanceFromResponse(t *testing.T) {
	router := newTestRouterWithTravelDistance(stubTravelDistanceLookup{
		view: traveldistanceview.TravelDistanceView{MetresM: 99, Known: true},
	})
	body := map[string]any{
		"plannedHeads":      3,
		"installedStations": 5,
		"rateUnitsPerHour":  50,
		"hours":             8,
		// No fromLocationCode/toLocationCode: the lookup must never be
		// attempted and the response must omit both fields entirely.
	}

	rec := doJSON(t, router, http.MethodPost, "/paths/pick-a/plan", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("got status %d, want 201, body=%s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if _, ok := resp["travelDistanceM"]; ok {
		t.Fatalf("expected travelDistanceM to be omitted, got %s", rec.Body.String())
	}
	if _, ok := resp["travelDistanceEstimated"]; ok {
		t.Fatalf("expected travelDistanceEstimated to be omitted, got %s", rec.Body.String())
	}
}

func TestPostShiftPlan_NoTravelDistanceLookupWired_BehavesExactlyAsBefore(t *testing.T) {
	// newTestRouter (the pre-existing helper, unmodified) never wires a
	// TravelDistanceLookup — asserts this feature did not change the
	// default, unenriched behaviour for the existing test suite.
	router := newTestRouter()
	body := map[string]any{
		"plannedHeads":      3,
		"installedStations": 5,
		"rateUnitsPerHour":  50,
		"hours":             8,
		"fromLocationCode":  "WH1-STOR-AMB-A07-01-01-A",
		"toLocationCode":    "WH1-STOR-AMB-A09-03-01-A",
	}

	rec := doJSON(t, router, http.MethodPost, "/paths/pick-a/plan", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("got status %d, want 201, body=%s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if _, ok := resp["travelDistanceM"]; ok {
		t.Fatalf("expected travelDistanceM to be omitted with no lookup wired, got %s", rec.Body.String())
	}
}
