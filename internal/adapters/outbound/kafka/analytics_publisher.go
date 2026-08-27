package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	kafkago "github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"

	"github.com/claudioed/wes-work-planning/internal/adapters/kafka/otelkafka"
	"github.com/claudioed/wes-work-planning/internal/application/ports"
	"github.com/claudioed/wes-work-planning/internal/domain/shared"
)

// AnalyticsTopic is the dedicated topic the analytics data product consumes.
// It is separate from the integration topic (envelope.TopicWorkPlanningEvents)
// so the OLTP integration contract and the analytical read-model stream evolve
// independently (ADR-0011).
const AnalyticsTopic = "warehouse.wes.analytics"

// analyticsSource identifies this service in the "source" field of every
// analytics envelope it publishes.
const analyticsSource = "wes-work-planning"

// analyticsSchemaVersion is the schema version stamped onto every analytics
// envelope this publisher emits.
const analyticsSchemaVersion = 1

// AnalyticsEnvelope is the Envelope v1 wrapper for the analytics stream.
// Unlike the integration envelope it carries the payload as a
// json.RawMessage so a single publisher can emit the event_type-specific
// data object for every domain event without a bespoke struct per type.
type AnalyticsEnvelope struct {
	EventId       string          `json:"event_id"`
	EventType     string          `json:"event_type"`
	OccurredAt    time.Time       `json:"occurred_at"`
	Source        string          `json:"source"`
	SchemaVersion int             `json:"schema_version"`
	Data          json.RawMessage `json:"data"`
}

// AnalyticsPublisher publishes every wes-work-planning domain event onto
// AnalyticsTopic as an AnalyticsEnvelope. It satisfies ports.EventPublisher
// and is a SEPARATE adapter from Publisher: the integration publisher
// (publisher.go) and warehouse.work-planning.events are left untouched.
//
// Every event this report is built from already carries its own PathId (the
// aggregate key), so no repo lookup is needed to populate the report's path
// dimension — the publisher stays thin (contrast the fulfillment pilot, whose
// task-scoped events needed a TaskRepo lookup to recover task_type; see
// ADR-0011).
type AnalyticsPublisher struct {
	writer Writer
	newID  IDGenerator
}

// NewAnalyticsPublisher constructs an AnalyticsPublisher writing to
// AnalyticsTopic on brokers. newID mints the envelope event_id.
func NewAnalyticsPublisher(brokers []string, newID IDGenerator) *AnalyticsPublisher {
	return NewAnalyticsPublisherWithWriter(&kafkago.Writer{
		Addr:                   kafkago.TCP(brokers...),
		Topic:                  AnalyticsTopic,
		Balancer:               &kafkago.LeastBytes{},
		AllowAutoTopicCreation: true,
	}, newID)
}

// NewAnalyticsPublisherWithWriter builds an AnalyticsPublisher against an
// already-constructed Writer — the seam unit tests use to substitute a fake
// without a real broker; production code should use NewAnalyticsPublisher.
func NewAnalyticsPublisherWithWriter(writer Writer, newID IDGenerator) *AnalyticsPublisher {
	return &AnalyticsPublisher{writer: writer, newID: newID}
}

// Close releases the underlying Kafka writer.
func (p *AnalyticsPublisher) Close() error {
	return p.writer.Close()
}

// Publish emits every event in events onto AnalyticsTopic. Events with no
// analytics payload (an unrecognised type) are skipped rather than erroring,
// so the caller can hand it the full event stream indiscriminately.
func (p *AnalyticsPublisher) Publish(ctx context.Context, events ...shared.DomainEvent) error {
	if len(events) == 0 {
		return nil
	}

	ctx, span := otelkafka.StartPublishSpan(ctx, AnalyticsTopic,
		semconv.MessagingBatchMessageCount(len(events)),
	)
	defer span.End()

	msgs := make([]kafkago.Message, 0, len(events))
	emitted := make([]string, 0, len(events))
	for _, e := range events {
		eventType, key, data, ok := marshalAnalyticsData(e)
		if !ok {
			continue
		}
		env := AnalyticsEnvelope{
			EventId:       p.newID(),
			EventType:     eventType,
			OccurredAt:    e.OccurredAt(),
			Source:        analyticsSource,
			SchemaVersion: analyticsSchemaVersion,
			Data:          data,
		}
		body, err := json.Marshal(env)
		if err != nil {
			return recordErr(span, fmt.Errorf("kafka: marshal analytics envelope: %w", err))
		}
		msg := kafkago.Message{Key: []byte(key), Value: body}
		otelkafka.Inject(ctx, &msg)
		msgs = append(msgs, msg)
		emitted = append(emitted, eventType)
	}

	if len(msgs) == 0 {
		return nil
	}

	span.SetAttributes(attribute.StringSlice("messaging.event_types", emitted))

	if err := p.writer.WriteMessages(ctx, msgs...); err != nil {
		return recordErr(span, fmt.Errorf("kafka: publish analytics events: %w", err))
	}
	return nil
}

// marshalAnalyticsData maps a domain event to its analytics event_type,
// aggregate-id message key, and snake_case JSON payload. The bool return is
// false for an event type outside the analytics contract, so Publish skips
// it. The message key is the aggregate id: PathId for path-scoped events and
// the WorkUnit id for work-unit events, so a partition holds an aggregate's
// events in order.
func marshalAnalyticsData(e shared.DomainEvent) (eventType, key string, data json.RawMessage, ok bool) {
	switch ev := e.(type) {
	case shared.WorkReleased:
		return "WorkReleased", ev.WorkUnitId, mustMarshal(map[string]any{
			"path_id":      ev.PathId.String(),
			"work_unit_id": ev.WorkUnitId,
		}), true
	case shared.WorkUnitCompleted:
		return "WorkUnitCompleted", ev.WorkUnitId, mustMarshal(map[string]any{
			"path_id":      ev.PathId.String(),
			"work_unit_id": ev.WorkUnitId,
		}), true
	case shared.WorkUnitCreated:
		return "WorkUnitCreated", ev.WorkUnitId, mustMarshal(map[string]any{
			"path_id":      ev.PathId.String(),
			"work_unit_id": ev.WorkUnitId,
		}), true
	case shared.BacklogThresholdBreached:
		return "BacklogThresholdBreached", ev.PathId.String(), mustMarshal(map[string]any{
			"path_id": ev.PathId.String(),
		}), true
	case shared.PathThrottled:
		return "PathThrottled", ev.PathId.String(), mustMarshal(map[string]any{
			"path_id": ev.PathId.String(),
		}), true
	case shared.RateDeviationDetected:
		return "RateDeviationDetected", ev.PathId.String(), mustMarshal(map[string]any{
			"path_id": ev.PathId.String(),
		}), true
	case shared.ChargeForecastReceived:
		return "ChargeForecastReceived", ev.PathId.String(), mustMarshal(map[string]any{
			"path_id": ev.PathId.String(),
		}), true
	case shared.ShiftPlanCommitted:
		return "ShiftPlanCommitted", ev.PathId.String(), mustMarshal(map[string]any{
			"path_id": ev.PathId.String(),
		}), true
	case shared.LaborReassignmentFlagged:
		return "LaborReassignmentFlagged", ev.PathId.String(), mustMarshal(map[string]any{
			"path_id": ev.PathId.String(),
		}), true
	default:
		return "", "", nil, false
	}
}

// mustMarshal marshals a map whose shape is fully controlled by
// marshalAnalyticsData, so an error here is a programming mistake rather than
// a runtime condition.
func mustMarshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("kafka: marshal analytics data: %v", err))
	}
	return b
}

// Compile-time assertion that AnalyticsPublisher satisfies the outbound
// event-publishing port.
var _ ports.EventPublisher = (*AnalyticsPublisher)(nil)
