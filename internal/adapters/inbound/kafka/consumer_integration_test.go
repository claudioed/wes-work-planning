//go:build integration

package kafka_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	kafkago "github.com/segmentio/kafka-go"

	inboundkafka "github.com/claudioed/wes-work-planning/internal/adapters/inbound/kafka"
	"github.com/claudioed/wes-work-planning/internal/adapters/kafka/envelope"
	"github.com/claudioed/wes-work-planning/internal/adapters/outbound/events"
	"github.com/claudioed/wes-work-planning/internal/adapters/outbound/memory"
	"github.com/claudioed/wes-work-planning/internal/application/ports"
	"github.com/claudioed/wes-work-planning/internal/application/usecases"
	"github.com/claudioed/wes-work-planning/internal/domain/shared"
	"github.com/claudioed/wes-work-planning/internal/domain/workunit"
)

// TestConsumer_ProjectsRealBrokerMessages requires KAFKA_BROKERS to point at
// a running broker (the shared broker at ~/warehouse-systems/docker-compose.kafka.yml
// on localhost:9092). It publishes a ShiftPlanCommitted-shaped message and a
// StockReserved-shaped message, runs the real Consumer against them, and
// asserts the read models update. Run with:
//
//	KAFKA_BROKERS=localhost:9092 go test -tags=integration ./internal/adapters/inbound/kafka/...
func TestConsumer_ProjectsRealBrokerMessages(t *testing.T) {
	brokersCSV := os.Getenv("KAFKA_BROKERS")
	if brokersCSV == "" {
		t.Skip("KAFKA_BROKERS not set; skipping kafka integration test")
	}
	brokers := strings.Split(brokersCSV, ",")

	pathIdValue := fmt.Sprintf("integration-kafka-pick-%d", time.Now().UnixNano())
	sku := fmt.Sprintf("integration-kafka-sku-%d", time.Now().UnixNano())
	workUnitIdValue := fmt.Sprintf("integration-kafka-wu-%d", time.Now().UnixNano())

	// Put the work unit into Released state via the existing use cases
	// before the TaskCompleted event arrives, so the consumer's call into
	// RecordCompletion has something valid to transition.
	workUnits := memory.NewWorkUnitRepo()
	pools := memory.NewWorkPoolRepo()
	publisher := events.NewLogPublisher(nil)
	clock := memory.SystemClock{}
	pathIdForUnit, err := shared.NewPathId(pathIdValue + "-wu")
	if err != nil {
		t.Fatalf("NewPathId: %v", err)
	}
	enqueue := usecases.NewEnqueueWorkUnit(workUnits, pools, publisher, clock)
	releaseNext := usecases.NewReleaseNextWork(pools, workUnits, publisher, clock)
	if _, err := enqueue.Execute(context.Background(), usecases.EnqueueWorkUnitRequest{
		WorkUnitId: workUnitIdValue, PathId: pathIdForUnit, CPT: shared.NewCPT(time.Now().Add(2 * time.Hour)), Reference: "ref-1",
	}); err != nil {
		t.Fatalf("EnqueueWorkUnit: %v", err)
	}
	if _, err := releaseNext.Execute(context.Background(), usecases.ReleaseNextWorkRequest{PathId: pathIdForUnit}); err != nil {
		t.Fatalf("ReleaseNextWork: %v", err)
	}

	publishCtx, publishCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer publishCancel()

	workforceWriter := &kafkago.Writer{Addr: kafkago.TCP(brokers...), Topic: envelope.TopicWorkforceEvents, AllowAutoTopicCreation: true}
	defer workforceWriter.Close()
	if err := workforceWriter.WriteMessages(publishCtx, kafkago.Message{
		Key: []byte("evt-shift-" + pathIdValue),
		Value: mustEnvelopeJSON(t, "evt-shift-"+pathIdValue, envelope.EventTypeShiftPlanCommitted, "workforce-management", map[string]any{
			"building_id": "bldg-1", "shift_id": "shift-1", "path_id": pathIdValue,
			"planned_heads": 6, "planned_rate": 100.0, "planned_hours": 8.0,
		}),
	}); err != nil {
		t.Fatalf("publish ShiftPlanCommitted: %v", err)
	}

	inventoryWriter := &kafkago.Writer{Addr: kafkago.TCP(brokers...), Topic: envelope.TopicInventoryEvents, AllowAutoTopicCreation: true}
	defer inventoryWriter.Close()
	if err := inventoryWriter.WriteMessages(publishCtx, kafkago.Message{
		Key: []byte("evt-reserve-" + sku),
		Value: mustEnvelopeJSON(t, "evt-reserve-"+sku, "StockReserved", "inventory-storage", map[string]any{
			"sku": sku, "quantity": 5, "demand_ref": "demand-1",
		}),
	}); err != nil {
		t.Fatalf("publish StockReserved: %v", err)
	}

	fulfillmentWriter := &kafkago.Writer{Addr: kafkago.TCP(brokers...), Topic: envelope.TopicFulfillmentEvents, AllowAutoTopicCreation: true}
	defer fulfillmentWriter.Close()
	if err := fulfillmentWriter.WriteMessages(publishCtx, kafkago.Message{
		Key: []byte("evt-task-" + workUnitIdValue),
		Value: mustEnvelopeJSON(t, "evt-task-"+workUnitIdValue, envelope.EventTypeTaskCompleted, "fulfillment-execution", map[string]any{
			"task_id": "task-1", "station_id": "station-1", "work_unit_id": workUnitIdValue,
		}),
	}); err != nil {
		t.Fatalf("publish TaskCompleted: %v", err)
	}

	orderId := fmt.Sprintf("integration-kafka-order-%d", time.Now().UnixNano())
	orderManagementWriter := &kafkago.Writer{Addr: kafkago.TCP(brokers...), Topic: envelope.TopicOrderManagementEvents, AllowAutoTopicCreation: true}
	defer orderManagementWriter.Close()
	if err := orderManagementWriter.WriteMessages(publishCtx, kafkago.Message{
		Key: []byte("evt-order-" + orderId),
		Value: mustEnvelopeJSON(t, "evt-order-"+orderId, envelope.EventTypeOrderAllocated, "order-management", map[string]any{
			"order_id": orderId, "promise_date": time.Now().Add(3 * time.Hour).UTC().Format(time.RFC3339),
			"lines": []map[string]any{{"line_no": 1, "sku": sku, "path_id": pathIdValue, "gift_wrap": false}},
		}),
	}); err != nil {
		t.Fatalf("publish OrderAllocated: %v", err)
	}

	laborViews := memory.NewLaborPlanViewRepo()
	inventoryViews := memory.NewInventoryViewRepo()
	processed := memory.NewProcessedEventRepo()
	observeLabor := usecases.NewObserveLaborPlan(laborViews, processed)
	observeInventory := usecases.NewObserveInventoryChange(inventoryViews, processed)
	recordCompletion := usecases.NewRecordCompletion(workUnits, publisher, clock)

	groupID := fmt.Sprintf("wes-integration-test-%d", time.Now().UnixNano())
	consumer := inboundkafka.NewConsumer(brokers, groupID, observeLabor, observeInventory, recordCompletion, enqueue, processed, nil)
	defer consumer.Close()

	consumeCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = consumer.Run(consumeCtx) }()

	pathId, err := shared.NewPathId(pathIdValue)
	if err != nil {
		t.Fatalf("NewPathId: %v", err)
	}

	deadline := time.Now().Add(30 * time.Second)
	for {
		view, err := laborViews.FindByPathId(context.Background(), pathId)
		if err == nil {
			if view.PlannedHeads != 6 {
				t.Fatalf("got planned heads %d, want 6", view.PlannedHeads)
			}
			break
		}
		if err != ports.ErrNotFound || time.Now().After(deadline) {
			t.Fatalf("labor plan view never projected: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
	}

	deadline = time.Now().Add(30 * time.Second)
	for {
		inv, err := inventoryViews.FindBySKU(context.Background(), sku)
		if err == nil {
			if inv.UsableQuantity != -5 {
				t.Fatalf("got usable quantity %d, want -5", inv.UsableQuantity)
			}
			break
		}
		if err != ports.ErrNotFound || time.Now().After(deadline) {
			t.Fatalf("inventory view never projected: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
	}

	deadline = time.Now().Add(30 * time.Second)
	for {
		unit, err := workUnits.FindById(context.Background(), workUnitIdValue)
		if err != nil {
			t.Fatalf("FindById: %v", err)
		}
		if unit.State() == workunit.Completed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("work unit never completed from TaskCompleted event, state=%v", unit.State())
		}
		time.Sleep(200 * time.Millisecond)
	}

	orderLineWorkUnitId := fmt.Sprintf("%s-line-1", orderId)
	deadline = time.Now().Add(30 * time.Second)
	for {
		unit, err := workUnits.FindById(context.Background(), orderLineWorkUnitId)
		if err == nil {
			if unit.SKU() != sku {
				t.Fatalf("got sku %q, want %q", unit.SKU(), sku)
			}
			break
		}
		if err != ports.ErrNotFound || time.Now().After(deadline) {
			t.Fatalf("work unit never enqueued from OrderAllocated event: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func mustEnvelopeJSON(t *testing.T, eventId, eventType, source string, data map[string]any) []byte {
	t.Helper()
	rawData, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal data: %v", err)
	}
	body, err := json.Marshal(envelope.Envelope{
		EventId: eventId, EventType: eventType, OccurredAt: time.Now().UTC(), Source: source, Data: rawData,
	})
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	return body
}
