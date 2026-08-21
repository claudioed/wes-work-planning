package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type ProcessedEventRepo struct {
	pool *pgxpool.Pool
}

func NewProcessedEventRepo(pool *pgxpool.Pool) *ProcessedEventRepo {
	return &ProcessedEventRepo{pool: pool}
}

func (r *ProcessedEventRepo) TryMarkProcessed(ctx context.Context, eventId string, processedAt time.Time) (bool, error) {
	tag, err := r.pool.Exec(ctx, `
		INSERT INTO processed_events (event_id, processed_at)
		VALUES ($1, $2)
		ON CONFLICT (event_id) DO NOTHING
	`, eventId, processedAt)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 0, nil
}
