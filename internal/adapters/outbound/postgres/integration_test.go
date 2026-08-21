//go:build integration

package postgres_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/claudioed/wes-work-planning/internal/adapters/outbound/postgres"
	"github.com/claudioed/wes-work-planning/internal/domain/charge"
	"github.com/claudioed/wes-work-planning/internal/domain/plan"
	"github.com/claudioed/wes-work-planning/internal/domain/release"
	"github.com/claudioed/wes-work-planning/internal/domain/shared"
	"github.com/claudioed/wes-work-planning/internal/domain/workunit"
)

// TestPostgresRepos requires DATABASE_URL to point at a Postgres instance
// with migrations/0001_init.up.sql already applied. Run with:
//
//	DATABASE_URL=postgres://... go test -tags=integration ./internal/adapters/outbound/postgres/...
func TestPostgresRepos(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping postgres integration test")
	}

	ctx := context.Background()
	pool, err := postgres.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	pathId, _ := shared.NewPathId("integration-pick-a")

	t.Run("charge repo round trip", func(t *testing.T) {
		repo := postgres.NewChargeRepo(pool)
		qty, _ := shared.NewQuantity(42)
		cpt := shared.NewCPT(time.Now().Add(time.Hour).Truncate(time.Microsecond))

		f, err := charge.NewChargeForecast(pathId, []charge.CPTBucket{{CPT: cpt, Quantity: qty}}, time.Now().Truncate(time.Microsecond))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if err := repo.Save(ctx, f); err != nil {
			t.Fatalf("save: %v", err)
		}

		got, err := repo.FindByPathId(ctx, pathId)
		if err != nil {
			t.Fatalf("find: %v", err)
		}
		if got.TotalQuantity().Value() != 42 {
			t.Fatalf("got total %d, want 42", got.TotalQuantity().Value())
		}
	})

	t.Run("plan repo round trip", func(t *testing.T) {
		repo := postgres.NewPlanRepo(pool)
		heads, _ := shared.NewStationCount(3)
		installed, _ := shared.NewStationCount(5)
		rate, _ := shared.NewRate(40)

		pp, err := plan.NewPathPlan(pathId, heads, installed, rate, 8)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		sp, err := plan.NewShiftPlan([]plan.PathPlan{pp})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if err := repo.Save(ctx, pathId, sp); err != nil {
			t.Fatalf("save: %v", err)
		}

		got, err := repo.FindByPathId(ctx, pathId)
		if err != nil {
			t.Fatalf("find: %v", err)
		}
		if _, ok := got.PathPlan(pathId); !ok {
			t.Fatalf("expected path plan to round trip")
		}
	})

	t.Run("work pool and work unit repo round trip", func(t *testing.T) {
		poolRepo := postgres.NewWorkPoolRepo(pool)
		unitRepo := postgres.NewWorkUnitRepo(pool)

		wp := release.NewWorkPool(pathId, release.ReleaseFed, 10, 5)
		cpt := shared.NewCPT(time.Now().Add(time.Hour).Truncate(time.Microsecond))
		if err := wp.Enqueue("integration-wu-1", cpt); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if err := poolRepo.Save(ctx, wp); err != nil {
			t.Fatalf("save pool: %v", err)
		}

		unit, err := workunit.NewWorkUnit("integration-wu-1", pathId, cpt, "ref-1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if err := unitRepo.Save(ctx, unit); err != nil {
			t.Fatalf("save unit: %v", err)
		}

		gotPool, err := poolRepo.FindByPathId(ctx, pathId)
		if err != nil {
			t.Fatalf("find pool: %v", err)
		}
		if gotPool.BacklogDepth() != 1 {
			t.Fatalf("got backlog depth %d, want 1", gotPool.BacklogDepth())
		}

		gotUnit, err := unitRepo.FindById(ctx, "integration-wu-1")
		if err != nil {
			t.Fatalf("find unit: %v", err)
		}
		if gotUnit.State() != workunit.Pending {
			t.Fatalf("got state %v, want Pending", gotUnit.State())
		}
	})
}
