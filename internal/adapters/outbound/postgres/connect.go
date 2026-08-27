package postgres

import (
	"context"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Connect opens a pgxpool.Pool against databaseURL with the OTel pgx tracer
// installed, so every query, batch, copy and connect becomes a child span of
// whatever span is active on the calling context. otelpgx records the
// normalized SQL statement, never the literal argument values.
func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	cfg.ConnConfig.Tracer = otelpgx.NewTracer()

	return pgxpool.NewWithConfig(ctx, cfg)
}
