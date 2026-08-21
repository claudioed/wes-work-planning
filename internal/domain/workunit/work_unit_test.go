package workunit

import (
	"errors"
	"testing"
	"time"

	"github.com/claudioed/wes-work-planning/internal/domain/shared"
)

func mustWorkUnit(t *testing.T) *WorkUnit {
	t.Helper()
	pathId, _ := shared.NewPathId("pick-a")
	cpt := shared.NewCPT(time.Now())
	w, err := NewWorkUnit("wu-1", pathId, cpt, "order-line-1")
	if err != nil {
		t.Fatalf("unexpected error building fixture: %v", err)
	}
	return w
}

func TestNewWorkUnit_ValidatesId(t *testing.T) {
	pathId, _ := shared.NewPathId("pick-a")
	cpt := shared.NewCPT(time.Now())
	if _, err := NewWorkUnit("", pathId, cpt, "ref"); !errors.Is(err, ErrEmptyId) {
		t.Fatalf("got err %v, want %v", err, ErrEmptyId)
	}
	if _, err := NewWorkUnit("wu-1", pathId, cpt, ""); !errors.Is(err, ErrEmptyReference) {
		t.Fatalf("got err %v, want %v", err, ErrEmptyReference)
	}
}

func TestWorkUnit_ReleaseAtMostOnce(t *testing.T) {
	w := mustWorkUnit(t)

	if err := w.Release(time.Now()); err != nil {
		t.Fatalf("unexpected error on first release: %v", err)
	}
	if w.State() != Released {
		t.Fatalf("got state %v, want Released", w.State())
	}

	if err := w.Release(time.Now()); !errors.Is(err, ErrAlreadyReleased) {
		t.Fatalf("got err %v, want %v", err, ErrAlreadyReleased)
	}
}

func TestWorkUnit_CannotCompleteBeforeRelease(t *testing.T) {
	w := mustWorkUnit(t)

	if err := w.Complete(time.Now()); !errors.Is(err, ErrNotReleased) {
		t.Fatalf("got err %v, want %v", err, ErrNotReleased)
	}
}

func TestWorkUnit_NoDoubleComplete(t *testing.T) {
	w := mustWorkUnit(t)

	if err := w.Release(time.Now()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := w.Complete(time.Now()); err != nil {
		t.Fatalf("unexpected error on first complete: %v", err)
	}
	if w.State() != Completed {
		t.Fatalf("got state %v, want Completed", w.State())
	}

	if err := w.Complete(time.Now()); !errors.Is(err, ErrAlreadyCompleted) {
		t.Fatalf("got err %v, want %v", err, ErrAlreadyCompleted)
	}
}
