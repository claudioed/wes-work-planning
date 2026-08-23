// Package kafka is the outbound Kafka adapter: it implements
// ports.EventPublisher on top of github.com/segmentio/kafka-go, serializing
// each domain event into the shared integration-event envelope and writing
// it to this service's own topic.
package kafka

import (
	"context"
	"encoding/json"
	"time"

	kafkago "github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/claudioed/wes-work-planning/internal/adapters/kafka/envelope"
	"github.com/claudioed/wes-work-planning/internal/adapters/kafka/otelkafka"
	"github.com/claudioed/wes-work-planning/internal/application/ports"
	"github.com/claudioed/wes-work-planning/internal/domain/shared"
)

// IDGenerator returns a fresh event ID (a UUID v4 in production).
type IDGenerator func() string

// Publisher publishes domain events to warehouse.work-planning.events.
type Publisher struct {
	writer    *kafkago.Writer
	newID     IDGenerator
	workUnits ports.WorkUnitRepo
}

// NewPublisher constructs a Publisher writing to TopicWorkPlanningEvents on
// brokers. workUnits is used to enrich WorkReleased events with the CPT and
// reference the published schema requires (the WorkReleased domain event
// itself only carries the work unit ID and path ID).
func NewPublisher(brokers []string, workUnits ports.WorkUnitRepo, newID IDGenerator) *Publisher {
	return &Publisher{
		writer: &kafkago.Writer{
			Addr:                   kafkago.TCP(brokers...),
			Topic:                  envelope.TopicWorkPlanningEvents,
			Balancer:               &kafkago.LeastBytes{},
			AllowAutoTopicCreation: true,
		},
		newID:     newID,
		workUnits: workUnits,
	}
}

func (p *Publisher) Close() error {
	return p.writer.Close()
}

// Publish writes one envelope per domain event inside a single
// "kafka.publish <topic>" producer span, injecting that span's W3C trace
// context into every message's headers so the consuming service continues
// the same distributed trace.
func (p *Publisher) Publish(ctx context.Context, events ...shared.DomainEvent) error {
	if len(events) == 0 {
		return nil
	}

	ctx, span := otelkafka.StartPublishSpan(ctx, envelope.TopicWorkPlanningEvents,
		semconv.MessagingBatchMessageCount(len(events)),
	)
	defer span.End()

	msgs := make([]kafkago.Message, 0, len(events))
	for _, e := range events {
		data, err := p.dataFor(ctx, e)
		if err != nil {
			return recordErr(span, err)
		}

		env := envelope.Envelope{
			EventId:    p.newID(),
			EventType:  e.EventName(),
			OccurredAt: e.OccurredAt(),
			Source:     envelope.Source,
			Data:       data,
		}
		body, err := json.Marshal(env)
		if err != nil {
			return recordErr(span, err)
		}

		msg := kafkago.Message{Key: []byte(env.EventId), Value: body}
		otelkafka.Inject(ctx, &msg)
		msgs = append(msgs, msg)
	}

	span.SetAttributes(attribute.StringSlice("messaging.event_types", eventTypes(events)))

	if err := p.writer.WriteMessages(ctx, msgs...); err != nil {
		return recordErr(span, err)
	}
	return nil
}

// recordErr marks span failed and returns err unchanged, so instrumentation
// never alters the error the caller sees.
func recordErr(span trace.Span, err error) error {
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	return err
}

func eventTypes(events []shared.DomainEvent) []string {
	out := make([]string, len(events))
	for i, e := range events {
		out[i] = e.EventName()
	}
	return out
}

// dataFor builds the event-type-specific "data" payload. Only WorkReleased
// has a documented downstream schema; other event types get a best-effort
// payload of their own fields.
func (p *Publisher) dataFor(ctx context.Context, e shared.DomainEvent) (json.RawMessage, error) {
	switch ev := e.(type) {
	case shared.WorkReleased:
		cpt, ref := "", ""
		if unit, err := p.workUnits.FindById(ctx, ev.WorkUnitId); err == nil {
			cpt = unit.CPT().Time().Format(time.RFC3339)
			ref = unit.Reference()
		}
		return json.Marshal(map[string]any{
			"path_id":      ev.PathId.String(),
			"work_unit_id": ev.WorkUnitId,
			"cpt":          cpt,
			"ref":          ref,
		})
	case shared.ChargeForecastReceived:
		return json.Marshal(map[string]any{"path_id": ev.PathId.String()})
	case shared.ShiftPlanCommitted:
		return json.Marshal(map[string]any{"path_id": ev.PathId.String()})
	case shared.WorkUnitCreated:
		return json.Marshal(map[string]any{"path_id": ev.PathId.String(), "work_unit_id": ev.WorkUnitId})
	case shared.BacklogThresholdBreached:
		return json.Marshal(map[string]any{"path_id": ev.PathId.String()})
	case shared.RateDeviationDetected:
		return json.Marshal(map[string]any{"path_id": ev.PathId.String()})
	case shared.PathThrottled:
		return json.Marshal(map[string]any{"path_id": ev.PathId.String()})
	case shared.LaborReassignmentFlagged:
		return json.Marshal(map[string]any{"path_id": ev.PathId.String()})
	case shared.WorkUnitCompleted:
		return json.Marshal(map[string]any{"path_id": ev.PathId.String(), "work_unit_id": ev.WorkUnitId})
	default:
		return json.Marshal(map[string]any{})
	}
}
