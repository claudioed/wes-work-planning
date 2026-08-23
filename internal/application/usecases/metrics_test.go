package usecases_test

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/claudioed/wes-work-planning/internal/application/usecases"
	"github.com/claudioed/wes-work-planning/internal/domain/shared"
)

// releasedCounterName is the business metric ReleaseNextWork records.
const releasedCounterName = "wes.work_units.released"

// TestReleaseNextWorkCountsReleasedWorkUnits proves the business metric is
// driven by the domain event — a work unit actually leaving the pool — and
// carries the process path as an attribute. It uses a manual reader so no
// collector is involved.
func TestReleaseNextWorkCountsReleasedWorkUnits(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))

	f := newFixture()
	enqueue := usecases.NewEnqueueWorkUnit(f.workUnits, f.pools, f.publisher, f.clock)
	releaseUC := usecases.NewReleaseNextWork(f.pools, f.workUnits, f.publisher, f.clock)
	pathId, _ := shared.NewPathId("metrics-path")

	ctx := context.Background()
	for _, id := range []string{"wu-1", "wu-2"} {
		if _, err := enqueue.Execute(ctx, usecases.EnqueueWorkUnitRequest{
			WorkUnitId: id,
			PathId:     pathId,
			CPT:        shared.NewCPT(f.clock.Now().Add(time.Hour)),
			Reference:  "ref-" + id,
		}); err != nil {
			t.Fatalf("unexpected error enqueuing %s: %v", id, err)
		}
	}

	for i := range 2 {
		if _, err := releaseUC.Execute(ctx, usecases.ReleaseNextWorkRequest{PathId: pathId}); err != nil {
			t.Fatalf("unexpected error on release %d: %v", i, err)
		}
	}

	var collected metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &collected); err != nil {
		t.Fatalf("collecting metrics: %v", err)
	}

	got, found := releasedCountFor(t, collected, pathId.String())
	if !found {
		t.Fatalf("no %s datapoint for %s=%s in %v", releasedCounterName, usecases.AttrPathId, pathId, collected.ScopeMetrics)
	}
	if got != 2 {
		t.Fatalf("%s{%s=%s} = %d, want 2", releasedCounterName, usecases.AttrPathId, pathId, got)
	}
}

// releasedCountFor digs the counter value for one path_id out of the
// collected metrics.
func releasedCountFor(t *testing.T, collected metricdata.ResourceMetrics, pathId string) (int64, bool) {
	t.Helper()
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != releasedCounterName {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s is a %T, want an int64 counter", m.Name, m.Data)
			}
			for _, dp := range sum.DataPoints {
				if v, ok := dp.Attributes.Value(attribute.Key(usecases.AttrPathId)); ok && v.String() == pathId {
					return dp.Value, true
				}
			}
		}
	}
	return 0, false
}
