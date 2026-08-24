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

// hazmatTag and fragileTag are the ProductClassification.HandlingTags
// values inventory-storage uses that this service maps onto the
// WorkReleased integration event's derived hints (see ADR-0009). Named as
// constants here — not shared as a Go type across the repository boundary —
// because this is exactly the same translate-at-the-ACL discipline this
// adapter already applies to every other cross-service payload.
const (
	hazmatTag  = "Hazmat"
	fragileTag = "Fragile"
)

// IDGenerator returns a fresh event ID (a UUID v4 in production).
type IDGenerator func() string

// Writer is the subset of *kafkago.Writer this adapter depends on, so unit
// tests can substitute a fake without a real broker (mirrors
// inventory-storage's own outbound/kafka.Writer interface).
type Writer interface {
	WriteMessages(ctx context.Context, msgs ...kafkago.Message) error
	Close() error
}

// Publisher publishes domain events to warehouse.work-planning.events.
type Publisher struct {
	writer          Writer
	newID           IDGenerator
	workUnits       ports.WorkUnitRepo
	classifications ports.ProductClassificationLookup
}

// NewPublisher constructs a Publisher writing to TopicWorkPlanningEvents on
// brokers. workUnits is used to enrich WorkReleased events with the CPT and
// reference the published schema requires (the WorkReleased domain event
// itself only carries the work unit ID and path ID). classifications is
// used to enrich WorkReleased with derived hazmat-capability/fragile hints
// by looking up the released unit's SKU once at publish time (see
// ADR-0009); a nil classifications is treated exactly like
// productclassification.PermissiveLookup — those two optional fields are
// simply omitted/false, so every existing caller of NewPublisher keeps
// compiling and behaving unchanged.
func NewPublisher(brokers []string, workUnits ports.WorkUnitRepo, classifications ports.ProductClassificationLookup, newID IDGenerator) *Publisher {
	return NewPublisherWithWriter(&kafkago.Writer{
		Addr:                   kafkago.TCP(brokers...),
		Topic:                  envelope.TopicWorkPlanningEvents,
		Balancer:               &kafkago.LeastBytes{},
		AllowAutoTopicCreation: true,
	}, workUnits, classifications, newID)
}

// NewPublisherWithWriter builds a Publisher against an already-constructed
// Writer — the seam unit tests use to substitute a fake without a real
// broker; production code should use NewPublisher.
func NewPublisherWithWriter(writer Writer, workUnits ports.WorkUnitRepo, classifications ports.ProductClassificationLookup, newID IDGenerator) *Publisher {
	return &Publisher{
		writer:          writer,
		newID:           newID,
		workUnits:       workUnits,
		classifications: classifications,
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
		cpt, ref, sku := "", "", ""
		if unit, err := p.workUnits.FindById(ctx, ev.WorkUnitId); err == nil {
			cpt = unit.CPT().Time().Format(time.RFC3339)
			ref = unit.Reference()
			sku = unit.SKU()
		}

		requiredCapabilities, fragile := p.classificationHints(ctx, sku)

		data := map[string]any{
			"path_id":      ev.PathId.String(),
			"work_unit_id": ev.WorkUnitId,
			"cpt":          cpt,
			"ref":          ref,
		}
		// Strictly additive and backward compatible: only set these two
		// optional fields when there is something to say. An unclassified
		// SKU or an unavailable lookup omits them entirely rather than
		// publishing an empty array / explicit false, so a consumer that
		// already treats "absent" as "no hint" (fulfillment-execution's
		// documented default) sees no difference from before this feature
		// existed.
		if len(requiredCapabilities) > 0 {
			data["required_capabilities"] = requiredCapabilities
		}
		if fragile {
			data["fragile"] = fragile
		}
		return json.Marshal(data)
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

// classificationHints looks up sku's ProductClassification once, at
// publish time, and derives the two optional WorkReleased hints from it:
// "hazmat" appended to requiredCapabilities when the SKU is classified
// Hazmat, and fragile=true when it is classified Fragile. This is the
// concrete implementation of ADR-0009's "read-once-at-release, stamp onto
// WorkReleased" decision — fulfillment-execution's Task then carries these
// hints without ever calling back to inventory-storage.
//
// A missing sku, a nil classifications port, an unclassified SKU (Known
// but no relevant tag, or altogether unknown), or a lookup error are all
// treated identically: no hints. This is deliberately permissive/fail-open
// — unlike inventory-storage's own StowStock placement check, a
// classification-lookup problem here must never block or delay releasing
// work; it can only omit an optional enrichment.
func (p *Publisher) classificationHints(ctx context.Context, sku string) (requiredCapabilities []string, fragile bool) {
	if sku == "" || p.classifications == nil {
		return nil, false
	}

	view, err := p.classifications.GetClassification(ctx, sku)
	if err != nil || !view.Known {
		return nil, false
	}

	if view.HasTag(hazmatTag) {
		requiredCapabilities = append(requiredCapabilities, "hazmat")
	}
	fragile = view.HasTag(fragileTag)
	return requiredCapabilities, fragile
}
