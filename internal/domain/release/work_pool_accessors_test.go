package release

import (
	"errors"
	"testing"
	"time"

	"github.com/claudioed/wes-work-planning/internal/domain/shared"
)

func TestWorkPool_Accessors(t *testing.T) {
	pathId, _ := shared.NewPathId("pick-a")
	pool := NewWorkPool(pathId, ReleaseFed, 5, 3)

	if !pool.PathId().Equals(pathId) {
		t.Fatalf("got PathId %v, want %v", pool.PathId(), pathId)
	}
	if pool.Mode() != ReleaseFed {
		t.Fatalf("got Mode %v, want %v", pool.Mode(), ReleaseFed)
	}
	if pool.WIPLimit() != 5 {
		t.Fatalf("got WIPLimit %d, want 5", pool.WIPLimit())
	}
	if pool.AlarmThreshold() != 3 {
		t.Fatalf("got AlarmThreshold %d, want 3", pool.AlarmThreshold())
	}
}

func TestWorkPool_Accessors_FlowFed(t *testing.T) {
	pathId, _ := shared.NewPathId("pack-b")
	pool := NewWorkPool(pathId, FlowFed, 0, 7)

	if pool.Mode() != FlowFed {
		t.Fatalf("got Mode %v, want %v", pool.Mode(), FlowFed)
	}
	if pool.AlarmThreshold() != 7 {
		t.Fatalf("got AlarmThreshold %d, want 7", pool.AlarmThreshold())
	}
}

func TestWorkPool_Entries(t *testing.T) {
	pathId, _ := shared.NewPathId("pick-a")
	pool := NewWorkPool(pathId, ReleaseFed, 10, 0)

	if entries := pool.Entries(); len(entries) != 0 {
		t.Fatalf("got %d entries, want 0", len(entries))
	}

	cpt1 := shared.NewCPT(time.Now())
	cpt2 := shared.NewCPT(time.Now().Add(time.Hour))

	if err := pool.Enqueue("wu-1", cpt1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := pool.Enqueue("wu-2", cpt2); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entries := pool.Entries()
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if entries[0].WorkUnitId != "wu-1" || entries[0].Released {
		t.Fatalf("got %+v, want wu-1 pending", entries[0])
	}
	if entries[1].WorkUnitId != "wu-2" || entries[1].Released {
		t.Fatalf("got %+v, want wu-2 pending", entries[1])
	}
	if !entries[0].CPT.Equals(cpt1) {
		t.Fatalf("got CPT %v, want %v", entries[0].CPT, cpt1)
	}

	if err := pool.Release("wu-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entries = pool.Entries()
	if !entries[0].Released {
		t.Fatalf("got %+v, want wu-1 released", entries[0])
	}
	if entries[1].Released {
		t.Fatalf("got %+v, want wu-2 still pending", entries[1])
	}
}

func TestWorkPool_Release_WIPLimitInvariant_ReleaseFed(t *testing.T) {
	pathId, _ := shared.NewPathId("pick-a")
	pool := NewWorkPool(pathId, ReleaseFed, 1, 0)
	cpt := shared.NewCPT(time.Now())

	if err := pool.Enqueue("wu-1", cpt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := pool.Enqueue("wu-2", cpt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := pool.Release("wu-1"); err != nil {
		t.Fatalf("unexpected error releasing first unit: %v", err)
	}

	if err := pool.Release("wu-2"); !errors.Is(err, ErrWIPLimitReached) {
		t.Fatalf("got err %v, want %v", err, ErrWIPLimitReached)
	}
}

func TestWorkPool_Release_WIPLimitNotEnforced_FlowFed(t *testing.T) {
	pathId, _ := shared.NewPathId("pick-a")
	pool := NewWorkPool(pathId, FlowFed, 1, 0)
	cpt := shared.NewCPT(time.Now())

	if err := pool.Enqueue("wu-1", cpt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := pool.Enqueue("wu-2", cpt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := pool.Release("wu-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := pool.Release("wu-2"); err != nil {
		t.Fatalf("flow-fed pool should not enforce WIP limit on Release: %v", err)
	}
}
