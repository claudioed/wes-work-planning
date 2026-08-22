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

	recordCompletion := usecases.NewRecordCompletion(workUnits, publisher, clock)

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
