package kafka

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/claudioed/wes-work-planning/internal/adapters/kafka/envelope"
	"github.com/claudioed/wes-work-planning/internal/adapters/outbound/events"
	"github.com/claudioed/wes-work-planning/internal/adapters/outbound/memory"
	"github.com/claudioed/wes-work-planning/internal/application/usecases"
	"github.com/claudioed/wes-work-planning/internal/domain/shared"
	"github.com/claudioed/wes-work-planning/internal/domain/workunit"
)

// fulfillmentFixture wires just enough of the real stack (in-memory repos,
// the existing EnqueueWorkUnit/ReleaseNextWork/RecordCompletion use cases) to
// exercise handleFulfillmentEvent without touching a broker.
type fulfillmentFixture struct {
	workUnits *memory.WorkUnitRepo
	processed *memory.ProcessedEventRepo
	consumer  *Consumer
}

func newFulfillmentFixture() fulfillmentFixture {
	workUnits := memory.NewWorkUnitRepo()
	pools := memory.NewWorkPoolRepo()
	publisher := events.NewLogPublisher(nil)
	clock := memory.FixedClock{At: time.Date(2026, 8, 21, 8, 0, 0, 0, time.UTC)}
	processed := memory.NewProcessedEventRepo()

	recordCompletion := usecases.NewRecordCompletion(workUnits, pools, publisher, clock)

	return fulfillmentFixture{
		workUnits: workUnits,
		processed: processed,
		consumer: &Consumer{
			recordCompletion: recordCompletion,
			processed:        processed,
		},
	}.withReleasedUnit(pools, publisher, clock, workUnits)
}

func (f fulfillmentFixture) withReleasedUnit(pools *memory.WorkPoolRepo, publisher *events.LogPublisher, clock memory.FixedClock, workUnits *memory.WorkUnitRepo) fulfillmentFixture {
	ctx := context.Background()
	pathId, _ := shared.NewPathId("pick-a")
	cpt := shared.NewCPT(clock.At.Add(2 * time.Hour))

	enqueue := usecases.NewEnqueueWorkUnit(workUnits, pools, publisher, clock)
	release := usecases.NewReleaseNextWork(pools, workUnits, publisher, clock)

	if _, err := enqueue.Execute(ctx, usecases.EnqueueWorkUnitRequest{WorkUnitId: "wu-1", PathId: pathId, CPT: cpt, Reference: "ref-1"}); err != nil {
		panic(err)
	}
	if _, err := release.Execute(ctx, usecases.ReleaseNextWorkRequest{PathId: pathId}); err != nil {
		panic(err)
	}
	return f
}

func taskCompletedEnvelope(t *testing.T, eventId, workUnitId string) envelope.Envelope {
	t.Helper()
	data, err := json.Marshal(taskCompletedData{TaskId: "task-1", StationId: "station-1", WorkUnitId: workUnitId})
	if err != nil {
		t.Fatalf("marshal data: %v", err)
	}
	return envelope.Envelope{
		EventId:    eventId,
		EventType:  envelope.EventTypeTaskCompleted,
		OccurredAt: time.Date(2026, 8, 21, 9, 0, 0, 0, time.UTC),
		Source:     "fulfillment-execution",
		Data:       data,
	}
}

func TestHandleFulfillmentEvent_CompletesTheWorkUnit(t *testing.T) {
	f := newFulfillmentFixture()

	if err := f.consumer.handleFulfillmentEvent(context.Background(), taskCompletedEnvelope(t, "evt-task-1", "wu-1")); err != nil {
		t.Fatalf("handleFulfillmentEvent: %v", err)
	}

	unit, err := f.workUnits.FindById(context.Background(), "wu-1")
	if err != nil {
		t.Fatalf("FindById: %v", err)
	}
	if unit.State() != workunit.Completed {
		t.Fatalf("expected work unit to be Completed, got %v", unit.State())
	}
}

func TestHandleFulfillmentEvent_IgnoresOtherEventTypes(t *testing.T) {
	f := newFulfillmentFixture()

	env := taskCompletedEnvelope(t, "evt-other", "wu-1")
	env.EventType = "SomethingElse"

	if err := f.consumer.handleFulfillmentEvent(context.Background(), env); err != nil {
		t.Fatalf("handleFulfillmentEvent: %v", err)
	}

	unit, err := f.workUnits.FindById(context.Background(), "wu-1")
	if err != nil {
		t.Fatalf("FindById: %v", err)
	}
	if unit.State() != workunit.Released {
		t.Fatalf("expected work unit to remain Released, got %v", unit.State())
	}
}

func TestHandleFulfillmentEvent_RedeliveryDoesNotInvokeRecordCompletionAgain(t *testing.T) {
	f := newFulfillmentFixture()
	env := taskCompletedEnvelope(t, "evt-task-dup", "wu-1")

	if err := f.consumer.handleFulfillmentEvent(context.Background(), env); err != nil {
		t.Fatalf("first handleFulfillmentEvent: %v", err)
	}

	// Redelivery of the same event_id must not call RecordCompletion again;
	// if it did, this would surface workunit.ErrAlreadyCompleted instead of
	// silently no-op'ing via the processed_events idempotency check.
	if err := f.consumer.handleFulfillmentEvent(context.Background(), env); err != nil {
		t.Fatalf("redelivered handleFulfillmentEvent: %v", err)
	}

	unit, err := f.workUnits.FindById(context.Background(), "wu-1")
	if err != nil {
		t.Fatalf("FindById: %v", err)
	}
	if unit.State() != workunit.Completed {
		t.Fatalf("expected work unit to be Completed, got %v", unit.State())
	}
}

// orderManagementFixture wires just enough of the real stack (in-memory
// repos, the existing EnqueueWorkUnit use case) to exercise
// handleOrderManagementEvent without touching a broker.
type orderManagementFixture struct {
	workUnits *memory.WorkUnitRepo
	pools     *memory.WorkPoolRepo
	processed *memory.ProcessedEventRepo
	consumer  *Consumer
}

func newOrderManagementFixture() orderManagementFixture {
	workUnits := memory.NewWorkUnitRepo()
	pools := memory.NewWorkPoolRepo()
	publisher := events.NewLogPublisher(nil)
	clock := memory.FixedClock{At: time.Date(2026, 8, 21, 8, 0, 0, 0, time.UTC)}
	processed := memory.NewProcessedEventRepo()

	enqueueWorkUnit := usecases.NewEnqueueWorkUnit(workUnits, pools, publisher, clock)

	return orderManagementFixture{
		workUnits: workUnits,
		pools:     pools,
		processed: processed,
		consumer: &Consumer{
			enqueueWorkUnit: enqueueWorkUnit,
			processed:       processed,
		},
	}
}

func orderAllocatedEnvelope(t *testing.T, eventId, eventType, orderId string, lines []orderLineData) envelope.Envelope {
	t.Helper()
	data, err := json.Marshal(orderAllocatedData{
		OrderId:     orderId,
		PromiseDate: time.Date(2026, 8, 21, 23, 0, 0, 0, time.UTC),
		Lines:       lines,
	})
	if err != nil {
		t.Fatalf("marshal data: %v", err)
	}
	return envelope.Envelope{
		EventId:    eventId,
		EventType:  eventType,
		OccurredAt: time.Date(2026, 8, 21, 9, 0, 0, 0, time.UTC),
		Source:     "order-management",
		Data:       data,
	}
}

func TestHandleOrderManagementEvent_EnqueuesOneWorkUnitPerLine(t *testing.T) {
	f := newOrderManagementFixture()
	lines := []orderLineData{
		{LineNo: 1, SKU: "SKU-1", PathId: "pick-a", GiftWrap: false},
		{LineNo: 2, SKU: "SKU-2", PathId: "pick-a", GiftWrap: true},
	}
	env := orderAllocatedEnvelope(t, "evt-order-1", envelope.EventTypeOrderAllocated, "order-1", lines)

	if err := f.consumer.handleOrderManagementEvent(context.Background(), env); err != nil {
		t.Fatalf("handleOrderManagementEvent: %v", err)
	}

	unit1, err := f.workUnits.FindById(context.Background(), "order-1-line-1")
	if err != nil {
		t.Fatalf("FindById line 1: %v", err)
	}
	if unit1.SKU() != "SKU-1" || unit1.GiftWrap() {
		t.Fatalf("unexpected unit1: sku=%v giftWrap=%v", unit1.SKU(), unit1.GiftWrap())
	}

	unit2, err := f.workUnits.FindById(context.Background(), "order-1-line-2")
	if err != nil {
		t.Fatalf("FindById line 2: %v", err)
	}
	if unit2.SKU() != "SKU-2" || !unit2.GiftWrap() {
		t.Fatalf("unexpected unit2: sku=%v giftWrap=%v", unit2.SKU(), unit2.GiftWrap())
	}
}

func TestHandleOrderManagementEvent_PartiallyAllocatedBehavesIdentically(t *testing.T) {
	f := newOrderManagementFixture()
	lines := []orderLineData{{LineNo: 1, SKU: "SKU-9", PathId: "pick-a", GiftWrap: false}}
	env := orderAllocatedEnvelope(t, "evt-order-partial", envelope.EventTypeOrderPartiallyAllocated, "order-9", lines)

	if err := f.consumer.handleOrderManagementEvent(context.Background(), env); err != nil {
		t.Fatalf("handleOrderManagementEvent: %v", err)
	}

	unit, err := f.workUnits.FindById(context.Background(), "order-9-line-1")
	if err != nil {
		t.Fatalf("FindById: %v", err)
	}
	if unit.SKU() != "SKU-9" {
		t.Fatalf("unexpected sku: %v", unit.SKU())
	}
}

func TestHandleOrderManagementEvent_IgnoresOtherEventTypes(t *testing.T) {
	f := newOrderManagementFixture()
	lines := []orderLineData{{LineNo: 1, SKU: "SKU-1", PathId: "pick-a"}}
	env := orderAllocatedEnvelope(t, "evt-order-other", "SomethingElse", "order-1", lines)

	if err := f.consumer.handleOrderManagementEvent(context.Background(), env); err != nil {
		t.Fatalf("handleOrderManagementEvent: %v", err)
	}

	if _, err := f.workUnits.FindById(context.Background(), "order-1-line-1"); err == nil {
		t.Fatalf("expected no work unit to be enqueued for an unknown event type")
	}
}

func TestHandleOrderManagementEvent_RedeliveryDoesNotEnqueueAgain(t *testing.T) {
	f := newOrderManagementFixture()
	lines := []orderLineData{{LineNo: 1, SKU: "SKU-1", PathId: "pick-a"}}
	env := orderAllocatedEnvelope(t, "evt-order-dup", envelope.EventTypeOrderAllocated, "order-1", lines)

	if err := f.consumer.handleOrderManagementEvent(context.Background(), env); err != nil {
		t.Fatalf("first handleOrderManagementEvent: %v", err)
	}
	// Redelivery of the same event_id must not call EnqueueWorkUnit again;
	// the processed_events idempotency check must short-circuit before any
	// enqueue attempt, so a genuine release.ErrDuplicateEntry from the pool
	// is never even reached on the common redelivery path.
	if err := f.consumer.handleOrderManagementEvent(context.Background(), env); err != nil {
		t.Fatalf("redelivered handleOrderManagementEvent: %v", err)
	}

	pool, err := f.pools.FindByPathId(context.Background(), mustPathId(t, "pick-a"))
	if err != nil {
		t.Fatalf("FindByPathId: %v", err)
	}
	if got := pool.BacklogDepth(); got != 1 {
		t.Fatalf("expected exactly one pending entry after redelivery, got %d", got)
	}
}

func mustPathId(t *testing.T, value string) shared.PathId {
	t.Helper()
	pathId, err := shared.NewPathId(value)
	if err != nil {
		t.Fatalf("NewPathId: %v", err)
	}
	return pathId
}
