package release

import (
	"errors"
	"testing"
	"time"

	"github.com/claudioed/wes-work-planning/internal/domain/shared"
)

func TestWorkPool_ReleasesInCPTPriorityOrder(t *testing.T) {
	pathId, _ := shared.NewPathId("pick-a")
	pool := NewWorkPool(pathId, ReleaseFed, 10, 0)

	late := shared.NewCPT(time.Now().Add(2 * time.Hour))
	early := shared.NewCPT(time.Now())
	mid := shared.NewCPT(time.Now().Add(time.Hour))

	if err := pool.Enqueue("wu-late", late); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := pool.Enqueue("wu-early", early); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := pool.Enqueue("wu-mid", mid); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	first, err := pool.ReleaseNext()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if first != "wu-early" {
		t.Fatalf("got %q, want wu-early", first)
	}

	second, err := pool.ReleaseNext()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if second != "wu-mid" {
		t.Fatalf("got %q, want wu-mid", second)
	}
}

func TestWorkPool_EnqueueRejectsDuplicates(t *testing.T) {
	pathId, _ := shared.NewPathId("pick-a")
	pool := NewWorkPool(pathId, ReleaseFed, 10, 0)
	cpt := shared.NewCPT(time.Now())

	if err := pool.Enqueue("wu-1", cpt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := pool.Enqueue("wu-1", cpt); !errors.Is(err, ErrDuplicateEntry) {
		t.Fatalf("got err %v, want %v", err, ErrDuplicateEntry)
	}
}

func TestWorkPool_AtMostOnceHandout(t *testing.T) {
	pathId, _ := shared.NewPathId("pick-a")
	pool := NewWorkPool(pathId, ReleaseFed, 10, 0)
	cpt := shared.NewCPT(time.Now())

	if err := pool.Enqueue("wu-1", cpt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := pool.Release("wu-1"); err != nil {
		t.Fatalf("unexpected error on first release: %v", err)
	}
	if err := pool.Release("wu-1"); !errors.Is(err, ErrAlreadyReleased) {
		t.Fatalf("got err %v, want %v", err, ErrAlreadyReleased)
	}
}

func TestWorkPool_ReleaseUnknownEntry(t *testing.T) {
	pathId, _ := shared.NewPathId("pick-a")
	pool := NewWorkPool(pathId, ReleaseFed, 10, 0)

	if err := pool.Release("does-not-exist"); !errors.Is(err, ErrUnknownEntry) {
		t.Fatalf("got err %v, want %v", err, ErrUnknownEntry)
	}
}

func TestWorkPool_WIPLimitInvariant_ReleaseFed(t *testing.T) {
	pathId, _ := shared.NewPathId("pick-a")
	pool := NewWorkPool(pathId, ReleaseFed, 1, 0)
	cpt := shared.NewCPT(time.Now())

	if err := pool.Enqueue("wu-1", cpt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := pool.Enqueue("wu-2", cpt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := pool.ReleaseNext(); err != nil {
		t.Fatalf("unexpected error releasing first unit: %v", err)
	}

	if _, err := pool.ReleaseNext(); !errors.Is(err, ErrWIPLimitReached) {
		t.Fatalf("got err %v, want %v", err, ErrWIPLimitReached)
	}
}

func TestWorkPool_WIPLimitNotEnforced_FlowFed(t *testing.T) {
	pathId, _ := shared.NewPathId("pick-a")
	pool := NewWorkPool(pathId, FlowFed, 1, 1)
	cpt := shared.NewCPT(time.Now())

	if err := pool.Enqueue("wu-1", cpt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := pool.Enqueue("wu-2", cpt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := pool.ReleaseNext(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := pool.ReleaseNext(); err != nil {
		t.Fatalf("flow-fed pool should not enforce WIP limit as invariant: %v", err)
	}
}

func TestWorkPool_ReleaseNextOnEmptyPool(t *testing.T) {
	pathId, _ := shared.NewPathId("pick-a")
	pool := NewWorkPool(pathId, ReleaseFed, 10, 0)

	if _, err := pool.ReleaseNext(); !errors.Is(err, ErrEmptyPool) {
		t.Fatalf("got err %v, want %v", err, ErrEmptyPool)
	}
}

func TestWorkPool_AlarmThresholdOnFlowFed(t *testing.T) {
	pathId, _ := shared.NewPathId("pick-a")
	pool := NewWorkPool(pathId, FlowFed, 0, 1)
	cpt := shared.NewCPT(time.Now())

	if err := pool.Enqueue("wu-1", cpt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pool.IsOverAlarmThreshold() {
		t.Fatalf("did not expect alarm threshold breach yet")
	}

	if err := pool.Enqueue("wu-2", cpt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !pool.IsOverAlarmThreshold() {
		t.Fatalf("expected alarm threshold breach")
	}
}
