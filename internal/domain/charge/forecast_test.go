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

func TestChargeForecast_PathId(t *testing.T) {
	pathId, _ := shared.NewPathId("pick-a")
	cpt := shared.NewCPT(time.Now())
	q, _ := shared.NewQuantity(10)

	f, err := NewChargeForecast(pathId, []CPTBucket{{CPT: cpt, Quantity: q}}, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := f.PathId(); !got.Equals(pathId) {
		t.Fatalf("got pathId %v, want %v", got, pathId)
	}
}

func TestChargeForecast_Buckets(t *testing.T) {
	pathId, _ := shared.NewPathId("pick-a")
	cpt1 := shared.NewCPT(time.Now())
	cpt2 := shared.NewCPT(time.Now().Add(time.Hour))
	q1, _ := shared.NewQuantity(100)
	q2, _ := shared.NewQuantity(50)
	in := []CPTBucket{{CPT: cpt1, Quantity: q1}, {CPT: cpt2, Quantity: q2}}

	f, err := NewChargeForecast(pathId, in, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := f.Buckets()
	if len(got) != len(in) {
		t.Fatalf("got %d buckets, want %d", len(got), len(in))
	}
	for i := range in {
		if !got[i].CPT.Equals(in[i].CPT) {
			t.Fatalf("bucket %d: got CPT %v, want %v", i, got[i].CPT, in[i].CPT)
		}
		if got[i].Quantity.Value() != in[i].Quantity.Value() {
			t.Fatalf("bucket %d: got quantity %d, want %d", i, got[i].Quantity.Value(), in[i].Quantity.Value())
		}
	}

	got[0].Quantity = q2
	again := f.Buckets()
	if again[0].Quantity.Value() != in[0].Quantity.Value() {
		t.Fatalf("mutating returned slice affected internal state: got %d, want %d", again[0].Quantity.Value(), in[0].Quantity.Value())
	}
}

func TestChargeForecast_ReceivedAt(t *testing.T) {
	pathId, _ := shared.NewPathId("pick-a")
	cpt := shared.NewCPT(time.Now())
	q, _ := shared.NewQuantity(10)
	receivedAt := time.Now().Add(-3 * time.Hour)

	f, err := NewChargeForecast(pathId, []CPTBucket{{CPT: cpt, Quantity: q}}, receivedAt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := f.ReceivedAt(); !got.Equal(receivedAt) {
		t.Fatalf("got receivedAt %v, want %v", got, receivedAt)
	}
}
