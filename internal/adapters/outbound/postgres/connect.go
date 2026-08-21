package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Connect opens a pgxpool.Pool against databaseURL.
func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	return pgxpool.New(ctx, databaseURL)
}
