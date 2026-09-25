package usecases_test

import (
	"context"
	"errors"
	"testing"

	"github.com/claudioed/wes-work-planning/internal/application/usecases"
	"github.com/claudioed/wes-work-planning/internal/domain/shared"
	"github.com/claudioed/wes-work-planning/internal/domain/traveldistanceview"
)

// fakeTravelDistanceLookup is a stub ports.TravelDistanceLookup for
// CommitShiftPlan tests, mirroring the fakeClassificationLookup pattern
// used for ReleaseNextWork's ADR-0009 enrichment tests.
type fakeTravelDistanceLookup struct {
	view traveldistanceview.TravelDistanceView
	err  error
	// calls records every (from, to) pair GetDistance was invoked with,
	// so a test can assert the lookup was (or was not) attempted.
	calls [][2]string
}

func (f *fakeTravelDistanceLookup) GetDistance(_ context.Context, from, to string) (traveldistanceview.TravelDistanceView, error) {
	f.calls = append(f.calls, [2]string{from, to})
	if f.err != nil {
		return traveldistanceview.TravelDistanceView{}, f.err
	}
	return f.view, nil
}

func TestCommitShiftPlan_TravelDistance_KnownEnrichesPathPlan(t *testing.T) {
	f := newFixture()
	lookup := &fakeTravelDistanceLookup{view: traveldistanceview.TravelDistanceView{
		MetresM: 4.4, Estimated: false, Known: true,
	}}
	uc := usecases.NewCommitShiftPlan(f.plans, f.publisher, f.clock).WithTravelDistanceLookup(lookup)
	pathId, _ := shared.NewPathId("pick-a")
	rate, _ := shared.NewRate(50)
	heads, _ := shared.NewStationCount(4)
	installed, _ := shared.NewStationCount(5)

	sp, err := uc.Execute(context.Background(), usecases.CommitShiftPlanRequest{
		PathId:            pathId,
		PlannedHeads:      heads,
		InstalledStations: installed,
		Rate:              rate,
		Hours:             8,
		FromLocationCode:  "WH1-STOR-AMB-A07-01-01-A",
		ToLocationCode:    "WH1-STOR-AMB-A09-03-01-A",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	pathPlan, ok := sp.PathPlan(pathId)
	if !ok {
		t.Fatalf("expected committed plan to be retrievable")
	}
	if !pathPlan.TravelDistanceKnown() {
		t.Fatal("expected TravelDistanceKnown to be true")
	}
	if pathPlan.TravelDistanceM() != 4.4 {
		t.Fatalf("got TravelDistanceM %v, want 4.4", pathPlan.TravelDistanceM())
	}
	if len(lookup.calls) != 1 || lookup.calls[0] != [2]string{"WH1-STOR-AMB-A07-01-01-A", "WH1-STOR-AMB-A09-03-01-A"} {
		t.Fatalf("expected exactly one GetDistance call with the given locations, got %v", lookup.calls)
	}
}

func TestCommitShiftPlan_TravelDistance_UnknownOmitsHint(t *testing.T) {
	f := newFixture()
	lookup := &fakeTravelDistanceLookup{view: traveldistanceview.TravelDistanceView{Known: false}}
	uc := usecases.NewCommitShiftPlan(f.plans, f.publisher, f.clock).WithTravelDistanceLookup(lookup)
	pathId, _ := shared.NewPathId("pick-a")
	rate, _ := shared.NewRate(50)
	heads, _ := shared.NewStationCount(4)
	installed, _ := shared.NewStationCount(5)

	sp, err := uc.Execute(context.Background(), usecases.CommitShiftPlanRequest{
		PathId:            pathId,
		PlannedHeads:      heads,
		InstalledStations: installed,
		Rate:              rate,
		Hours:             8,
		FromLocationCode:  "WH1-STOR-AMB-A07-01-01-A",
		ToLocationCode:    "NOPE",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	pathPlan, _ := sp.PathPlan(pathId)
	if pathPlan.TravelDistanceKnown() {
		t.Fatal("expected TravelDistanceKnown to stay false when the lookup reports Known=false")
	}
}

func TestCommitShiftPlan_TravelDistance_LookupErrorFailsOpen(t *testing.T) {
	f := newFixture()
	lookup := &fakeTravelDistanceLookup{err: errors.New("facility-layout unavailable")}
	uc := usecases.NewCommitShiftPlan(f.plans, f.publisher, f.clock).WithTravelDistanceLookup(lookup)
	pathId, _ := shared.NewPathId("pick-a")
	rate, _ := shared.NewRate(50)
	heads, _ := shared.NewStationCount(4)
	installed, _ := shared.NewStationCount(5)

	sp, err := uc.Execute(context.Background(), usecases.CommitShiftPlanRequest{
		PathId:            pathId,
		PlannedHeads:      heads,
		InstalledStations: installed,
		Rate:              rate,
		Hours:             8,
		FromLocationCode:  "WH1-STOR-AMB-A07-01-01-A",
		ToLocationCode:    "WH1-STOR-AMB-A09-03-01-A",
	})
	if err != nil {
		t.Fatalf("expected Execute to still succeed on a lookup error (fail-open), got: %v", err)
	}
	pathPlan, _ := sp.PathPlan(pathId)
	if pathPlan.TravelDistanceKnown() {
		t.Fatal("expected TravelDistanceKnown to stay false when the lookup errors")
	}
}

func TestCommitShiftPlan_TravelDistance_MissingLocationCodesSkipsLookup(t *testing.T) {
	f := newFixture()
	lookup := &fakeTravelDistanceLookup{view: traveldistanceview.TravelDistanceView{MetresM: 99, Known: true}}
	uc := usecases.NewCommitShiftPlan(f.plans, f.publisher, f.clock).WithTravelDistanceLookup(lookup)
	pathId, _ := shared.NewPathId("pick-a")
	rate, _ := shared.NewRate(50)
	heads, _ := shared.NewStationCount(4)
	installed, _ := shared.NewStationCount(5)

	sp, err := uc.Execute(context.Background(), usecases.CommitShiftPlanRequest{
		PathId:            pathId,
		PlannedHeads:      heads,
		InstalledStations: installed,
		Rate:              rate,
		Hours:             8,
		// FromLocationCode/ToLocationCode both empty: no lookup attempt.
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lookup.calls) != 0 {
		t.Fatalf("expected GetDistance not to be called when locations are omitted, got %v", lookup.calls)
	}
	pathPlan, _ := sp.PathPlan(pathId)
	if pathPlan.TravelDistanceKnown() {
		t.Fatal("expected TravelDistanceKnown to stay false when no locations were supplied")
	}
}

func TestCommitShiftPlan_TravelDistance_NilLookupSkipsEnrichment(t *testing.T) {
	f := newFixture()
	// No WithTravelDistanceLookup call at all — mirrors every existing
	// caller/test that predates this feature.
	uc := usecases.NewCommitShiftPlan(f.plans, f.publisher, f.clock)
	pathId, _ := shared.NewPathId("pick-a")
	rate, _ := shared.NewRate(50)
	heads, _ := shared.NewStationCount(4)
	installed, _ := shared.NewStationCount(5)

	sp, err := uc.Execute(context.Background(), usecases.CommitShiftPlanRequest{
		PathId:            pathId,
		PlannedHeads:      heads,
		InstalledStations: installed,
		Rate:              rate,
		Hours:             8,
		FromLocationCode:  "WH1-STOR-AMB-A07-01-01-A",
		ToLocationCode:    "WH1-STOR-AMB-A09-03-01-A",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	pathPlan, _ := sp.PathPlan(pathId)
	if pathPlan.TravelDistanceKnown() {
		t.Fatal("expected TravelDistanceKnown to stay false with no lookup wired")
	}
}
