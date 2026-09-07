-- Transactional outbox (ADR-0014). Each row is ONE already-encoded Kafka
-- message for ONE topic: a domain event fanned out to both the integration
-- topic (warehouse.work-planning.events) and the analytics topic
-- (warehouse.wes.analytics) produces two rows. The use case inserts them
-- in the same transaction as the aggregate change; the in-process relay
-- drains them onto the broker in id order.
CREATE TABLE outbox_events (
    id           BIGSERIAL PRIMARY KEY,
    topic        TEXT        NOT NULL,
    event_type   TEXT        NOT NULL,
    key          BYTEA,
    value        BYTEA       NOT NULL,
    headers      JSONB       NOT NULL DEFAULT '[]',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at TIMESTAMPTZ,
    attempts     INTEGER     NOT NULL DEFAULT 0,
    last_error   TEXT
);

CREATE INDEX idx_outbox_events_unpublished ON outbox_events (id) WHERE published_at IS NULL;
