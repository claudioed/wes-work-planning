package charge

import (
	"errors"
	"testing"
	"time"

	"github.com/claudioed/wes-work-planning/internal/domain/shared"
)

func TestNewChargeForecast_RequiresBuckets(t *testing.T) {
	pathId, _ := shared.NewPathId("pick-a")
	_, err := NewChargeForecast(pathId, nil, time.Now())
	if !errors.Is(err, ErrNoBuckets) {
		t.Fatalf("got err %v, want %v", err, ErrNoBuckets)
	}
}

func TestChargeForecast_TotalAndLookup(t *testing.T) {
	pathId, _ := shared.NewPathId("pick-a")
	cpt1 := shared.NewCPT(time.Now())
	cpt2 := shared.NewCPT(time.Now().Add(time.Hour))
	q1, _ := shared.NewQuantity(100)
	q2, _ := shared.NewQuantity(50)

	f, err := NewChargeForecast(pathId, []CPTBucket{{CPT: cpt1, Quantity: q1}, {CPT: cpt2, Quantity: q2}}, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := f.TotalQuantity().Value(); got != 150 {
		t.Fatalf("got total %d, want 150", got)
	}

	got, err := f.QuantityForCPT(cpt1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Value() != 100 {
		t.Fatalf("got %d, want 100", got.Value())
	}

	unknownCPT := shared.NewCPT(time.Now().Add(48 * time.Hour))
	if _, err := f.QuantityForCPT(unknownCPT); !errors.Is(err, ErrUnknownCPT) {
		t.Fatalf("got err %v, want %v", err, ErrUnknownCPT)
	}
}
