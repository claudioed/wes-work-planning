package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	outboundkafka "github.com/claudioed/wes-work-planning/internal/adapters/outbound/kafka"
	"github.com/claudioed/wes-work-planning/internal/domain/shared"
)

// outboxHeader is the JSON shape of one Kafka header persisted in
// outbox_events.headers: [{"key":"traceparent","value":"00-..."}].
type outboxHeader struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// OutboxPublisher implements ports.EventPublisher by writing each event's
// Kafka wire form — for EVERY configured topic — into outbox_events
// instead of the broker (ADR-0014). One domain event becomes one row per
// Encoder that produces a message for it: the integration topic's row and
// the analytics topic's row are inserted side by side and later relayed
// in that order.
//
// Encoding happens here, inside the use case's transaction when called
// under UnitOfWork.Execute. That is deliberate: the integration
// Publisher's WorkReleased payload READS the WorkUnit repo, and only
// inside the transaction does that read see the row the use case just
// saved. OutboxPublisher never touches Kafka.
type OutboxPublisher struct {
	pool     *pgxpool.Pool
	encoders []outboundkafka.Encoder
}

// NewOutboxPublisher constructs an OutboxPublisher over pool that fans
// every event through encoders, in order. A nil encoder is skipped so the
// composition root can pass an optional one without a branch.
func NewOutboxPublisher(pool *pgxpool.Pool, encoders ...outboundkafka.Encoder) *OutboxPublisher {
	kept := make([]outboundkafka.Encoder, 0, len(encoders))
	for _, e := range encoders {
		if e != nil {
			kept = append(kept, e)
		}
	}
	return &OutboxPublisher{pool: pool, encoders: kept}
}

// Publish encodes events for every topic and stores the results in the
// outbox, joining the transaction bound to ctx when there is one.
func (p *OutboxPublisher) Publish(ctx context.Context, events ...shared.DomainEvent) error {
	if len(events) == 0 {
		return nil
	}
	q := querierFrom(ctx, p.pool)
	for _, enc := range p.encoders {
		msgs, err := enc.Encode(ctx, events...)
		if err != nil {
			return fmt.Errorf("postgres: encode outbox events: %w", err)
		}
		for _, m := range msgs {
			headers, err := marshalHeaders(m)
			if err != nil {
				return err
			}
			if _, err := q.Exec(ctx, `
				INSERT INTO outbox_events (topic, event_type, key, value, headers)
				VALUES ($1, $2, $3, $4, $5)
			`, m.Topic, m.EventType, m.Key, m.Value, headers); err != nil {
				return fmt.Errorf("postgres: enqueue outbox event %s for %s: %w", m.EventType, m.Topic, err)
			}
		}
	}
	return nil
}

func marshalHeaders(m outboundkafka.Encoded) ([]byte, error) {
	hs := make([]outboxHeader, len(m.Headers))
	for i, h := range m.Headers {
		hs[i] = outboxHeader{Key: h.Key, Value: string(h.Value)}
	}
	b, err := json.Marshal(hs)
	if err != nil {
		return nil, fmt.Errorf("postgres: marshal outbox headers for %s: %w", m.EventType, err)
	}
	return b, nil
}
