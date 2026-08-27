package analyticsstore

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/claudioed/wes-work-planning/internal/analytics/report"
)

// PostgresProjection is the WRITER implementation of report.ProjectionStore,
// backed by a pgxpool over the analytical database. Every Apply* runs in a
// transaction that first claims the event id in analytics_processed_events
// (ON CONFLICT DO NOTHING); it only mutates the rollup when the claim is new,
// making each apply idempotent per eventId under Kafka's at-least-once
// delivery. It is the only writer of the analytical database.
type PostgresProjection struct {
	pool *pgxpool.Pool
}

// NewPostgresProjection constructs a PostgresProjection over pool.
func NewPostgresProjection(pool *pgxpool.Pool) *PostgresProjection {
	return &PostgresProjection{pool: pool}
}

// claim inserts eventId into analytics_processed_events, returning true iff
// this call newly recorded it (so the caller should apply the effect). It
// runs inside tx so the claim and the effect commit atomically.
func claim(ctx context.Context, tx pgx.Tx, eventId string, occurredAt time.Time) (bool, error) {
	tag, err := tx.Exec(ctx,
		`INSERT INTO analytics_processed_events (event_id, occurred_at)
		 VALUES ($1, $2) ON CONFLICT (event_id) DO NOTHING`,
		eventId, occurredAt)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// inTx runs fn in a transaction, committing on success and rolling back on
// error.
func (p *PostgresProjection) inTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

// counter names the throughput_rollup column a single event increments.
type counter string

const (
	colWorkReleased             counter = "work_released"
	colWorkUnitCompleted        counter = "work_unit_completed"
	colBacklogThresholdBreached counter = "backlog_threshold_breached"
	colPathThrottled            counter = "path_throttled"
	colRateDeviationDetected    counter = "rate_deviation_detected"
)

// applyCounter is the shared body of every Apply* method: claim the event id
// idempotently, then bump exactly one rollup counter for the (path, hour)
// row. col is one of the package-private counter constants (never client
// input), so interpolating it into the statement is safe.
func (p *PostgresProjection) applyCounter(ctx context.Context, eventId, pathId string, at time.Time, col counter) error {
	return p.inTx(ctx, func(tx pgx.Tx) error {
		isNew, err := claim(ctx, tx, eventId, at)
		if err != nil {
			return fmt.Errorf("analyticsstore: claim event: %w", err)
		}
		if !isNew {
			return nil
		}
		return upsertCounter(ctx, tx, pathId, at, col)
	})
}

// ApplyWorkReleased increments the WorkReleased counter. Idempotent on eventId.
func (p *PostgresProjection) ApplyWorkReleased(ctx context.Context, eventId, pathId string, at time.Time) error {
	return p.applyCounter(ctx, eventId, pathId, at, colWorkReleased)
}

// ApplyWorkUnitCompleted increments the WorkUnitCompleted counter. Idempotent
// on eventId.
func (p *PostgresProjection) ApplyWorkUnitCompleted(ctx context.Context, eventId, pathId string, at time.Time) error {
	return p.applyCounter(ctx, eventId, pathId, at, colWorkUnitCompleted)
}

// ApplyBacklogThresholdBreached increments the BacklogThresholdBreached
// counter. Idempotent on eventId.
func (p *PostgresProjection) ApplyBacklogThresholdBreached(ctx context.Context, eventId, pathId string, at time.Time) error {
	return p.applyCounter(ctx, eventId, pathId, at, colBacklogThresholdBreached)
}

// ApplyPathThrottled increments the PathThrottled counter. Idempotent on
// eventId.
func (p *PostgresProjection) ApplyPathThrottled(ctx context.Context, eventId, pathId string, at time.Time) error {
	return p.applyCounter(ctx, eventId, pathId, at, colPathThrottled)
}

// ApplyRateDeviationDetected increments the RateDeviationDetected counter.
// Idempotent on eventId.
func (p *PostgresProjection) ApplyRateDeviationDetected(ctx context.Context, eventId, pathId string, at time.Time) error {
	return p.applyCounter(ctx, eventId, pathId, at, colRateDeviationDetected)
}

// upsertCounter adds 1 to the named counter column of the (path_id,
// hour_bucket) row, inserting the row (all counters zero) if absent.
// hour_bucket is derived by truncating at to the UTC hour. col is a trusted
// package constant, never client input.
func upsertCounter(ctx context.Context, tx pgx.Tx, pathId string, at time.Time, col counter) error {
	bucket := at.UTC().Truncate(time.Hour)
	stmt := fmt.Sprintf(
		`INSERT INTO throughput_rollup (path_id, hour_bucket, %[1]s)
		 VALUES ($1, $2, 1)
		 ON CONFLICT (path_id, hour_bucket) DO UPDATE SET
			%[1]s = throughput_rollup.%[1]s + 1`, col)
	if _, err := tx.Exec(ctx, stmt, pathId, bucket); err != nil {
		return fmt.Errorf("analyticsstore: upsert %s: %w", col, err)
	}
	return nil
}

// Compile-time assertion that PostgresProjection satisfies the write port.
var _ report.ProjectionStore = (*PostgresProjection)(nil)
