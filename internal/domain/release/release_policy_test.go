package release

import (
	"testing"
	"time"

	"github.com/claudioed/wes-work-planning/internal/domain/shared"
)

func TestReleasePolicy_AppliesPriorityOrder(t *testing.T) {
	pathId, _ := shared.NewPathId("pick-a")
	pool := NewWorkPool(pathId, ReleaseFed, 10, 0)

	early := shared.NewCPT(time.Now())
	late := shared.NewCPT(time.Now().Add(time.Hour))

	if err := pool.Enqueue("wu-late", late); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := pool.Enqueue("wu-early", early); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	policy := NewReleasePolicy()
	got, err := policy.Apply(pool)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "wu-early" {
		t.Fatalf("got %q, want wu-early", got)
	}
}
