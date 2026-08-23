// Package kafka is the inbound Kafka adapter: it consumes the integration
// events this service subscribes to and calls the additive projector use
// cases that maintain the labor-plan and usable-inventory read models.
package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"

	kafkago "github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/claudioed/wes-work-planning/internal/adapters/kafka/envelope"
	"github.com/claudioed/wes-work-planning/internal/adapters/kafka/otelkafka"
	"github.com/claudioed/wes-work-planning/internal/application/ports"
	"github.com/claudioed/wes-work-planning/internal/application/usecases"
	"github.com/claudioed/wes-work-planning/internal/domain/shared"
)

// shiftPlanCommittedData is Workforce Management's ShiftPlanCommitted
// payload — a DIFFERENT model from this service's own ShiftPlan aggregate.
type shiftPlanCommittedData struct {
	BuildingId   string  `json:"building_id"`
	ShiftId      string  `json:"shift_id"`
	PathId       string  `json:"path_id"`
	PlannedHeads int     `json:"planned_heads"`
	PlannedRate  float64 `json:"planned_rate"`
	PlannedHours float64 `json:"planned_hours"`
}

// inventoryEventData is shared by StockReserved and ReservationRevoked.
type inventoryEventData struct {
	SKU       string `json:"sku"`
	Quantity  int    `json:"quantity"`
	DemandRef string `json:"demand_ref"`
}

// taskCompletedData is fulfillment-execution's TaskCompleted payload.
type taskCompletedData struct {
	TaskId     string `json:"task_id"`
	StationId  string `json:"station_id"`
	WorkUnitId string `json:"work_unit_id"`
}

// Consumer consumes warehouse.workforce.events, warehouse.inventory.events,
// and warehouse.fulfillment.events. The first two are projected into the
// labor-plan-view and inventory-view read models; TaskCompleted from the
// third is fed directly into the existing RecordCompletion use case to close
// the control loop's feedback edge from Execution back to this service.
type Consumer struct {
	workforceReader   *kafkago.Reader
	inventoryReader   *kafkago.Reader
	fulfillmentReader *kafkago.Reader
	observeLabor      *usecases.ObserveLaborPlan
	observeInventory  *usecases.ObserveInventoryChange
	recordCompletion  *usecases.RecordCompletion
	processed         ports.ProcessedEventRepo
	logger            *slog.Logger
}

func NewConsumer(brokers []string, groupID string, observeLabor *usecases.ObserveLaborPlan, observeInventory *usecases.ObserveInventoryChange, recordCompletion *usecases.RecordCompletion, processed ports.ProcessedEventRepo, logger *slog.Logger) *Consumer {
	return &Consumer{
		workforceReader: kafkago.NewReader(kafkago.ReaderConfig{
			Brokers: brokers,
			GroupID: groupID,
			Topic:   envelope.TopicWorkforceEvents,
		}),
		inventoryReader: kafkago.NewReader(kafkago.ReaderConfig{
			Brokers: brokers,
			GroupID: groupID,
			Topic:   envelope.TopicInventoryEvents,
		}),
		fulfillmentReader: kafkago.NewReader(kafkago.ReaderConfig{
			Brokers: brokers,
			GroupID: groupID,
			Topic:   envelope.TopicFulfillmentEvents,
		}),
		observeLabor:     observeLabor,
		observeInventory: observeInventory,
		recordCompletion: recordCompletion,
		processed:        processed,
		logger:           logger,
	}
}

func (c *Consumer) Close() error {
	err1 := c.workforceReader.Close()
	err2 := c.inventoryReader.Close()
	err3 := c.fulfillmentReader.Close()
	return errors.Join(err1, err2, err3)
}

// Run consumes all three topics until ctx is cancelled.
func (c *Consumer) Run(ctx context.Context) error {
	errCh := make(chan error, 3)
	go func() { errCh <- c.consumeLoop(ctx, c.workforceReader, c.handleWorkforceEvent) }()
	go func() { errCh <- c.consumeLoop(ctx, c.inventoryReader, c.handleInventoryEvent) }()
	go func() { errCh <- c.consumeLoop(ctx, c.fulfillmentReader, c.handleFulfillmentEvent) }()

	for i := 0; i < 3; i++ {
		if err := <-errCh; err != nil {
			return err
		}
	}
	return nil
}

func (c *Consumer) consumeLoop(ctx context.Context, reader *kafkago.Reader, handle func(context.Context, envelope.Envelope) error) error {
	for {
		msg, err := reader.FetchMessage(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		}

		if err := c.handleMessage(ctx, reader, msg, handle); err != nil {
			return err
		}
	}
}

// handleMessage processes one fetched message inside a
// "kafka.consume <topic>" span whose parent is the producing service's
// publish span, recovered from the message's W3C trace-context headers.
// Unparseable or unhandleable messages are logged and committed rather than
// redelivered forever; only a commit failure aborts the consume loop, which
// is the error this returns.
func (c *Consumer) handleMessage(ctx context.Context, reader *kafkago.Reader, msg kafkago.Message, handle func(context.Context, envelope.Envelope) error) error {
	topic := reader.Config().Topic

	msgCtx, span := otelkafka.StartConsumeSpan(otelkafka.Extract(ctx, &msg), topic,
		semconv.MessagingKafkaOffset(int(msg.Offset)),
		semconv.MessagingDestinationPartitionID(strconv.Itoa(msg.Partition)),
	)
	defer span.End()

	var env envelope.Envelope
	if err := json.Unmarshal(msg.Value, &env); err != nil {
		recordSpanError(span, err)
		c.log(msgCtx, "skipping unparseable kafka message", "topic", topic, "error", err)
		_ = reader.CommitMessages(ctx, msg)
		return nil
	}

	span.SetAttributes(
		attribute.String("messaging.message.event_id", env.EventId),
		attribute.String("messaging.message.event_type", env.EventType),
		attribute.String("messaging.message.source", env.Source),
	)

	if err := handle(msgCtx, env); err != nil {
		recordSpanError(span, err)
		c.log(msgCtx, "skipping kafka event",
			"topic", topic, "event_id", env.EventId, "event_type", env.EventType, "error", err)
		_ = reader.CommitMessages(ctx, msg)
		return nil
	}

	if err := reader.CommitMessages(ctx, msg); err != nil {
		recordSpanError(span, err)
		return err
	}
	return nil
}

// recordSpanError marks span as failed without changing any control flow.
func recordSpanError(span trace.Span, err error) {
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}

func (c *Consumer) handleWorkforceEvent(ctx context.Context, env envelope.Envelope) error {
	if env.EventType != envelope.EventTypeShiftPlanCommitted {
		return nil
	}

	var data shiftPlanCommittedData
	if err := json.Unmarshal(env.Data, &data); err != nil {
		return err
	}
	pathId, err := shared.NewPathId(data.PathId)
	if err != nil {
		return err
	}

	return c.observeLabor.Execute(ctx, usecases.ObserveLaborPlanRequest{
		EventId:      env.EventId,
		PathId:       pathId,
		PlannedHeads: data.PlannedHeads,
		PlannedRate:  data.PlannedRate,
		PlannedHours: data.PlannedHours,
		ObservedAt:   env.OccurredAt,
	})
}

func (c *Consumer) handleInventoryEvent(ctx context.Context, env envelope.Envelope) error {
	var delta int
	switch env.EventType {
	case envelope.EventTypeStockReserved:
		delta = -1
	case envelope.EventTypeReservationRevoked:
		delta = 1
	default:
		return nil
	}

	var data inventoryEventData
	if err := json.Unmarshal(env.Data, &data); err != nil {
		return err
	}

	_, err := c.observeInventory.Execute(ctx, usecases.ObserveInventoryChangeRequest{
		EventId:    env.EventId,
		SKU:        data.SKU,
		Quantity:   data.Quantity,
		Delta:      delta * data.Quantity,
		ObservedAt: env.OccurredAt,
	})
	return err
}

// handleFulfillmentEvent filters for TaskCompleted and feeds it into the
// existing RecordCompletion use case. Idempotency reuses the same
// processed_events mechanism as the Task 7 projectors: RecordCompletion
// itself already rejects a double-complete at the domain level, but marking
// the event_id here avoids a spurious error/retry on mere redelivery.
func (c *Consumer) handleFulfillmentEvent(ctx context.Context, env envelope.Envelope) error {
	if env.EventType != envelope.EventTypeTaskCompleted {
		return nil
	}

	var data taskCompletedData
	if err := json.Unmarshal(env.Data, &data); err != nil {
		return err
	}

	alreadyProcessed, err := c.processed.TryMarkProcessed(ctx, env.EventId, env.OccurredAt)
	if err != nil {
		return err
	}
	if alreadyProcessed {
		return nil
	}

	_, err = c.recordCompletion.Execute(ctx, usecases.RecordCompletionRequest{WorkUnitId: data.WorkUnitId})
	return err
}

// log emits a structured record through the configured logger, carrying the
// consume span's trace_id/span_id via ctx. A nil logger silences output, as
// the tests rely on.
func (c *Consumer) log(ctx context.Context, msg string, args ...any) {
	if c.logger != nil {
		c.logger.WarnContext(ctx, msg, args...)
	}
}
