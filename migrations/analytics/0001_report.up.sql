-- Work-planning "Release Throughput & Backlog Health" analytics read model
-- (ADR-0011).
--
-- This is the ANALYTICAL database, separate from the OLTP database. It is
-- written only by cmd/wes-projector and read (read-only) by cmd/wes-reports.
-- The tables here are projections derived from the analytics event stream,
-- not sources of truth.

-- Idempotency + freshness: every applied analytics event id is recorded
-- here exactly once. applied_at is wall-clock insert time; occurred_at is
-- the event's business time, used to compute the projection's freshness lag.
CREATE TABLE analytics_processed_events (
    event_id    TEXT PRIMARY KEY,
    occurred_at TIMESTAMPTZ NOT NULL,
    applied_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_analytics_processed_events_occurred_at
    ON analytics_processed_events (occurred_at DESC);

-- Consumer-level dedupe set, used by the inbound consumer's ProcessedEvents
-- gate. It is kept SEPARATE from analytics_processed_events (which the
-- projection UPSERT claims) so the two idempotency layers do not race to
-- claim the same event_id: the consumer gate admits the event, the
-- projection then records its effect.
CREATE TABLE analytics_consumed_events (
    event_id     TEXT PRIMARY KEY,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The throughput rollup fact table: one row per (path_id, hour_bucket).
-- Each counter is UPSERTed as the matching analytics event arrives.
CREATE TABLE throughput_rollup (
    path_id                    TEXT NOT NULL,
    hour_bucket                TIMESTAMPTZ NOT NULL,
    work_released              BIGINT NOT NULL DEFAULT 0,
    work_unit_completed        BIGINT NOT NULL DEFAULT 0,
    backlog_threshold_breached BIGINT NOT NULL DEFAULT 0,
    path_throttled             BIGINT NOT NULL DEFAULT 0,
    rate_deviation_detected    BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (path_id, hour_bucket)
);

CREATE INDEX idx_throughput_rollup_hour_bucket
    ON throughput_rollup (hour_bucket);
