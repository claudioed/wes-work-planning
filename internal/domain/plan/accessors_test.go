package plan

import (
	"testing"

	"github.com/claudioed/wes-work-planning/internal/domain/shared"
)

func TestPathPlan_Accessors(t *testing.T) {
	pathId, _ := shared.NewPathId("pick-a")
	heads, _ := shared.NewStationCount(3)
	installed, _ := shared.NewStationCount(5)
	rate, _ := shared.NewRate(50)

	p, err := NewPathPlan(pathId, heads, installed, rate, 8)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !p.PlannedHeads().LessThan(installed) || p.PlannedHeads().Value() != 3 {
		t.Fatalf("got PlannedHeads %v, want 3", p.PlannedHeads().Value())
	}
	if p.InstalledStations().Value() != 5 {
		t.Fatalf("got InstalledStations %v, want 5", p.InstalledStations().Value())
	}
	if p.Rate().UnitsPerHour() != 50 {
		t.Fatalf("got Rate %v, want 50", p.Rate().UnitsPerHour())
	}
	if p.Hours() != 8 {
		t.Fatalf("got Hours %v, want 8", p.Hours())
	}
}

func TestShiftPlan_PathPlans(t *testing.T) {
	pp1 := mustPathPlan(t, "pick-a", 3, 5)
	pp2 := mustPathPlan(t, "pack-b", 2, 4)

	sp, err := NewShiftPlan([]PathPlan{pp1, pp2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := sp.PathPlans()
	if len(got) != 2 {
		t.Fatalf("got %d path plans, want 2", len(got))
	}
	if !got[0].PathId().Equals(pp1.PathId()) || !got[1].PathId().Equals(pp2.PathId()) {
		t.Fatalf("got wrong path plans: %+v", got)
	}

	got[0] = mustPathPlan(t, "mutated", 1, 1)
	again := sp.PathPlans()
	if again[0].PathId().Equals(got[0].PathId()) {
		t.Fatalf("expected PathPlans to return a defensive copy")
	}
}

func TestShiftPlan_TotalHours(t *testing.T) {
	pp1 := mustPathPlan(t, "pick-a", 3, 5)
	pp2 := mustPathPlan(t, "pack-b", 2, 4)

	sp, err := NewShiftPlan([]PathPlan{pp1, pp2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := pp1.Hours() + pp2.Hours()
	if got := sp.TotalHours(); got != want {
		t.Fatalf("got TotalHours %v, want %v", got, want)
	}
}
