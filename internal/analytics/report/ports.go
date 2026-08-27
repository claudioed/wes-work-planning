package report

import (
	"context"
	"time"
)

// ReportStore is the read side of the throughput data product: the reader
// process queries it to serve reports. It is read-only by contract — the
// Postgres implementation runs over a pool pinned to a read-only role.
type ReportStore interface {
	// Query returns the throughput rows matching q.
	Query(ctx context.Context, q ReportQuery) (ThroughputReport, error)
	// FreshnessLag reports how far the read model lags real time: the age
	// of the most recently applied event. A larger lag means the projection
	// is further behind the event stream.
	FreshnessLag(ctx context.Context) (time.Duration, error)
}

// ProjectionStore is the write side of the throughput data product: the
// projector process applies each consumed event to it. Every Apply* method
// is idempotent on eventId — applying the same eventId twice records the
// effect once, so the at-least-once Kafka stream can be projected exactly
// once.
//
// The methods take the derivation-relevant fields already extracted from the
// analytics envelope (rather than a domain event) so this port stays free of
// any OLTP domain dependency. Every event this report is built from carries
// its own PathId, so no repo lookup is needed to populate the path
// dimension.
type ProjectionStore interface {
	// ApplyWorkReleased records that a work unit was released into pathId at
	// `at`, incrementing the (pathId, hour) row's WorkReleased counter.
	ApplyWorkReleased(ctx context.Context, eventId, pathId string, at time.Time) error
	// ApplyWorkUnitCompleted records that a released work unit in pathId
	// completed at `at`, incrementing the row's WorkUnitCompleted counter.
	ApplyWorkUnitCompleted(ctx context.Context, eventId, pathId string, at time.Time) error
	// ApplyBacklogThresholdBreached records a backlog-threshold breach for
	// pathId at `at`.
	ApplyBacklogThresholdBreached(ctx context.Context, eventId, pathId string, at time.Time) error
	// ApplyPathThrottled records a path-throttle decision for pathId at `at`.
	ApplyPathThrottled(ctx context.Context, eventId, pathId string, at time.Time) error
	// ApplyRateDeviationDetected records a rate-deviation detection for
	// pathId at `at`.
	ApplyRateDeviationDetected(ctx context.Context, eventId, pathId string, at time.Time) error
}

// ProcessedEvents is the consumer-side idempotency gate for the analytics
// projector: it records each consumed analytics event id exactly once so
// Kafka's at-least-once redelivery does not double-apply an effect. It lives
// in this read-model region (not the OLTP application/ports package) so the
// analytics consumer never depends on the OLTP layers.
type ProcessedEvents interface {
	// MarkProcessed records eventId if absent, returning true iff this call
	// newly recorded it (so the caller should apply the effect) and false
	// when it was already seen (skip).
	MarkProcessed(ctx context.Context, eventId string) (bool, error)
}
