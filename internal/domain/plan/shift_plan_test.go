package plan

import (
	"errors"
	"testing"

	"github.com/claudioed/wes-work-planning/internal/domain/shared"
)

func mustPathPlan(t *testing.T, pathId string, heads, installed int) PathPlan {
	t.Helper()
	pid, _ := shared.NewPathId(pathId)
	h, _ := shared.NewStationCount(heads)
	i, _ := shared.NewStationCount(installed)
	rate, _ := shared.NewRate(50)
	p, err := NewPathPlan(pid, h, i, rate, 8)
	if err != nil {
		t.Fatalf("unexpected error building fixture path plan: %v", err)
	}
	return p
}

func TestNewShiftPlan_RequiresAtLeastOnePathPlan(t *testing.T) {
	_, err := NewShiftPlan(nil)
	if !errors.Is(err, ErrNoPathPlans) {
		t.Fatalf("got err %v, want %v", err, ErrNoPathPlans)
	}
}

func TestNewShiftPlan_LooksUpPathPlan(t *testing.T) {
	pp := mustPathPlan(t, "pick-a", 3, 5)
	sp, err := NewShiftPlan([]PathPlan{pp})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pathId, _ := shared.NewPathId("pick-a")
	got, ok := sp.PathPlan(pathId)
	if !ok {
		t.Fatalf("expected path plan to be found")
	}
	if !got.PathId().Equals(pathId) {
		t.Fatalf("got wrong path plan")
	}

	unknown, _ := shared.NewPathId("pack-b")
	if _, ok := sp.PathPlan(unknown); ok {
		t.Fatalf("expected unknown path to not be found")
	}
}
