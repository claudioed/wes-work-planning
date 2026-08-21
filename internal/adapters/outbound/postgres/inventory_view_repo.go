package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/claudioed/wes-work-planning/internal/application/ports"
	"github.com/claudioed/wes-work-planning/internal/domain/inventoryview"
)

type InventoryViewRepo struct {
	pool *pgxpool.Pool
}

func NewInventoryViewRepo(pool *pgxpool.Pool) *InventoryViewRepo {
	return &InventoryViewRepo{pool: pool}
}

func (r *InventoryViewRepo) ApplyDelta(ctx context.Context, sku string, delta int, observedAt time.Time) (inventoryview.UsableInventoryObserved, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO usable_inventory_view (sku, usable_quantity, observed_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (sku) DO UPDATE SET
			usable_quantity = usable_inventory_view.usable_quantity + $2, observed_at = $3
		RETURNING usable_quantity, observed_at
	`, sku, delta, observedAt)

	v := inventoryview.UsableInventoryObserved{SKU: sku}
	if err := row.Scan(&v.UsableQuantity, &v.ObservedAt); err != nil {
		return inventoryview.UsableInventoryObserved{}, err
	}
	return v, nil
}

func (r *InventoryViewRepo) FindBySKU(ctx context.Context, sku string) (inventoryview.UsableInventoryObserved, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT usable_quantity, observed_at FROM usable_inventory_view WHERE sku = $1
	`, sku)

	v := inventoryview.UsableInventoryObserved{SKU: sku}
	if err := row.Scan(&v.UsableQuantity, &v.ObservedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return inventoryview.UsableInventoryObserved{}, ports.ErrNotFound
		}
		return inventoryview.UsableInventoryObserved{}, err
	}
	return v, nil
}
