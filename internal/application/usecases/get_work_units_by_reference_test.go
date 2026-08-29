package usecases_test

import (
	"context"
	"testing"
	"time"

	"github.com/claudioed/wes-work-planning/internal/application/usecases"
	"github.com/claudioed/wes-work-planning/internal/domain/shared"
)

func TestGetWorkUnitsByReference_ReturnsAllMatches(t *testing.T) {
	f := newFixture()
	enqueue := usecases.NewEnqueueWorkUnit(f.workUnits, f.pools, f.publisher, f.clock)
	uc := usecases.NewGetWorkUnitsByReference(f.workUnits)
	pathId, _ := shared.NewPathId("pick-a")
	cpt := shared.NewCPT(f.clock.Now().Add(time.Hour))

	if _, err := enqueue.Execute(context.Background(), usecases.EnqueueWorkUnitRequest{
		WorkUnitId: "wu-1",
		PathId:     pathId,
		CPT:        cpt,
		Reference:  "order-77213-line-1",
	}); err != nil {
		t.Fatalf("setup: unexpected error: %v", err)
	}
	if _, err := enqueue.Execute(context.Background(), usecases.EnqueueWorkUnitRequest{
		WorkUnitId: "wu-2",
		PathId:     pathId,
		CPT:        cpt,
		Reference:  "order-77213-line-1",
	}); err != nil {
		t.Fatalf("setup: unexpected error: %v", err)
	}
	if _, err := enqueue.Execute(context.Background(), usecases.EnqueueWorkUnitRequest{
		WorkUnitId: "wu-3",
		PathId:     pathId,
		CPT:        cpt,
		Reference:  "order-99999-line-1",
	}); err != nil {
		t.Fatalf("setup: unexpected error: %v", err)
	}

	units, err := uc.Execute(context.Background(), usecases.GetWorkUnitsByReferenceRequest{Reference: "order-77213-line-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(units) != 2 {
		t.Fatalf("got %d units, want 2", len(units))
	}
	for _, u := range units {
		if u.Reference() != "order-77213-line-1" {
			t.Fatalf("got reference %q, want order-77213-line-1", u.Reference())
		}
	}
}

func TestGetWorkUnitsByReference_NoMatchesReturnsEmpty(t *testing.T) {
	f := newFixture()
	uc := usecases.NewGetWorkUnitsByReference(f.workUnits)

	units, err := uc.Execute(context.Background(), usecases.GetWorkUnitsByReferenceRequest{Reference: "unknown-ref"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(units) != 0 {
		t.Fatalf("got %d units, want 0", len(units))
	}
}
