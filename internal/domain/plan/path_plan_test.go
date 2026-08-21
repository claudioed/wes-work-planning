package plan

import (
	"errors"
	"testing"

	"github.com/claudioed/wes-work-planning/internal/domain/shared"
)

func TestNewPathPlan(t *testing.T) {
	pathId, _ := shared.NewPathId("pick-a")
	rate, _ := shared.NewRate(50)

	tests := []struct {
		name              string
		plannedHeads      int
		installedStations int
		hours             float64
		wantErr           error
	}{
		{"heads within installed stations", 3, 5, 8, nil},
		{"heads equal installed stations", 5, 5, 8, nil},
		{"heads exceed installed stations", 6, 5, 8, ErrHeadsExceedStations},
		{"zero hours invalid", 3, 5, 0, shared.ErrInvalidHours},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			heads, _ := shared.NewStationCount(tt.plannedHeads)
			installed, _ := shared.NewStationCount(tt.installedStations)

			_, err := NewPathPlan(pathId, heads, installed, rate, tt.hours)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("got err %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestPathPlan_PlannedThroughput(t *testing.T) {
	pathId, _ := shared.NewPathId("pick-a")
	rate, _ := shared.NewRate(50)
	heads, _ := shared.NewStationCount(4)
	installed, _ := shared.NewStationCount(4)

	p, err := NewPathPlan(pathId, heads, installed, rate, 8)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := 50.0 * 4 * 8
	if got := p.PlannedThroughput(); got != want {
		t.Fatalf("got throughput %v, want %v", got, want)
	}
}
