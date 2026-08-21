// Package kafka is the inbound Kafka adapter: it consumes the integration
// events this service subscribes to and calls the additive projector use
// cases that maintain the labor-plan and usable-inventory read models.
package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"log"

	kafkago "github.com/segmentio/kafka-go"

	"github.com/claudioed/wes-work-planning/internal/adapters/kafka/envelope"
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

// Consumer consumes warehouse.workforce.events and warehouse.inventory.events
// and projects them into the labor-plan-view and inventory-view read models.
type Consumer struct {
	workforceReader  *kafkago.Reader
	inventoryReader  *kafkago.Reader
	observeLabor     *usecases.ObserveLaborPlan
	observeInventory *usecases.ObserveInventoryChange
	logger           *log.Logger
}

func NewConsumer(brokers []string, groupID string, observeLabor *usecases.ObserveLaborPlan, observeInventory *usecases.ObserveInventoryChange, logger *log.Logger) *Consumer {
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
		observeLabor:     observeLabor,
		observeInventory: observeInventory,
		logger:           logger,
	}
}

func (c *Consumer) Close() error {
	err1 := c.workforceReader.Close()
	err2 := c.inventoryReader.Close()
	return errors.Join(err1, err2)
}

// Run consumes both topics until ctx is cancelled.
func (c *Consumer) Run(ctx context.Context) error {
	errCh := make(chan error, 2)
	go func() { errCh <- c.consumeLoop(ctx, c.workforceReader, c.handleWorkforceEvent) }()
	go func() { errCh <- c.consumeLoop(ctx, c.inventoryReader, c.handleInventoryEvent) }()

	for i := 0; i < 2; i++ {
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

		var env envelope.Envelope
		if err := json.Unmarshal(msg.Value, &env); err != nil {
			c.logf("skipping unparseable message on %s: %v", reader.Config().Topic, err)
			_ = reader.CommitMessages(ctx, msg)
			continue
		}

		if err := handle(ctx, env); err != nil {
			c.logf("skipping event_id=%s event_type=%s on %s: %v", env.EventId, env.EventType, reader.Config().Topic, err)
			_ = reader.CommitMessages(ctx, msg)
			continue
		}

		if err := reader.CommitMessages(ctx, msg); err != nil {
			return err
		}
	}
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

func (c *Consumer) logf(format string, args ...any) {
	if c.logger != nil {
		c.logger.Printf(format, args...)
	}
}
