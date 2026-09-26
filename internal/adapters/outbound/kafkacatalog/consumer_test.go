package kafkacatalog

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	kafkago "github.com/segmentio/kafka-go"

	"github.com/claudioed/wes-work-planning/internal/domain/pathcatalog"
)

// fakeReader replays a fixed sequence of messages, then blocks until ctx
// is cancelled -- mirrors a real Kafka reader that has caught up and is
// now waiting for new messages.
type fakeReader struct {
	messages []kafkago.Message
	pos      int
	closed   bool
}

func (r *fakeReader) ReadMessage(ctx context.Context) (kafkago.Message, error) {
	if r.pos < len(r.messages) {
		m := r.messages[r.pos]
		r.pos++
		return m, nil
	}
	<-ctx.Done()
	return kafkago.Message{}, ctx.Err()
}

func (r *fakeReader) Close() error {
	r.closed = true
	return nil
}

func envelopeMsg(t *testing.T, partition int, offset int64, eventType string, data any) kafkago.Message {
	t.Helper()
	rawData, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal data: %v", err)
	}
	env := envelope{EventType: eventType, Data: rawData}
	rawEnv, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	return kafkago.Message{Partition: partition, Offset: offset, Value: rawEnv}
}

func newTestConsumer(reader Reader, target targetOffsets) *Consumer {
	c := &Consumer{
		Reader:  reader,
		paths:   make(map[string]pathcatalog.PathDefinition),
		readyCh: make(chan struct{}),
		target:  target,
	}
	if len(target) == 0 {
		c.markReady()
	}
	return c
}

func TestConsumer_NoTargetOffsets_IsReadyImmediately(t *testing.T) {
	c := newTestConsumer(&fakeReader{}, targetOffsets{})
	if !c.Ready() {
		t.Fatal("expected a consumer with no readiness target to be ready immediately")
	}
}

func TestConsumer_Run_BecomesReadyAfterCatchingUpSinglePartition(t *testing.T) {
	reader := &fakeReader{
		messages: []kafkago.Message{
			envelopeMsg(t, 0, 0, eventTypeCreated, pathData{PathId: "PICK", MatchPrefix: "pick", Direct: true, RequiredCapabilities: []string{"pick"}}),
			envelopeMsg(t, 0, 1, eventTypeCreated, pathData{PathId: "PACK", MatchPrefix: "pack", Direct: true, RequiredCapabilities: []string{"pack"}}),
		},
	}
	// target[0] = 2 means "caught up once offset 1 has been processed"
	// (last is exclusive: 2 messages at offsets 0 and 1).
	c := newTestConsumer(reader, targetOffsets{0: 2})

	if c.Ready() {
		t.Fatal("expected not ready before Run starts")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		_ = c.Run(ctx)
		close(done)
	}()

	if err := c.WaitReady(ctx); err != nil {
		t.Fatalf("expected Ready before timeout, got: %v", err)
	}
	cancel()
	<-done

	if _, err := c.Lookup("pick"); err != nil {
		t.Fatalf("expected PICK to be looked up successfully, got: %v", err)
	}
	if _, err := c.Lookup("pack"); err != nil {
		t.Fatalf("expected PACK to be looked up successfully, got: %v", err)
	}
}

func TestConsumer_MultiPartition_ReadyOnlyAfterBothCaughtUp(t *testing.T) {
	reader := &fakeReader{
		messages: []kafkago.Message{
			envelopeMsg(t, 0, 0, eventTypeCreated, pathData{PathId: "PICK", MatchPrefix: "pick", Direct: true, RequiredCapabilities: []string{"pick"}}),
			// Partition 1 not yet caught up (target[1]=1, need offset 0).
		},
	}
	c := newTestConsumer(reader, targetOffsets{0: 1, 1: 1})

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	go func() { _ = c.Run(ctx) }()

	if err := c.WaitReady(ctx); err == nil {
		t.Fatal("expected WaitReady to time out -- partition 1 never caught up")
	}
}

func TestConsumer_Deactivated_RemovesPathFromCache(t *testing.T) {
	reader := &fakeReader{
		messages: []kafkago.Message{
			envelopeMsg(t, 0, 0, eventTypeCreated, pathData{PathId: "PICK", MatchPrefix: "pick", Direct: true, RequiredCapabilities: []string{"pick"}}),
			envelopeMsg(t, 0, 1, eventTypeDeactivated, pathData{PathId: "PICK"}),
		},
	}
	c := newTestConsumer(reader, targetOffsets{0: 2})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() { _ = c.Run(ctx) }()

	if err := c.WaitReady(ctx); err != nil {
		t.Fatalf("expected Ready before timeout, got: %v", err)
	}

	if _, err := c.Lookup("pick"); err == nil {
		t.Fatal("expected PICK to be unknown after deactivation")
	}
}

func TestConsumer_Revised_UpdatesMatchPrefix(t *testing.T) {
	reader := &fakeReader{
		messages: []kafkago.Message{
			envelopeMsg(t, 0, 0, eventTypeCreated, pathData{PathId: "PICK", MatchPrefix: "pick", Direct: true, RequiredCapabilities: []string{"pick"}}),
			envelopeMsg(t, 0, 1, eventTypeUpdated, pathData{PathId: "PICK", MatchPrefix: "pick-zone-a", Direct: true, RequiredCapabilities: []string{"pick", "hazmat"}}),
		},
	}
	c := newTestConsumer(reader, targetOffsets{0: 2})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() { _ = c.Run(ctx) }()

	if err := c.WaitReady(ctx); err != nil {
		t.Fatalf("expected Ready before timeout, got: %v", err)
	}

	def, err := c.Lookup("pick-zone-a")
	if err != nil {
		t.Fatalf("expected pick-zone-a to resolve after revision, got: %v", err)
	}
	if len(def.RequiredCapabilities) != 2 {
		t.Fatalf("expected 2 required capabilities after revision, got %d", len(def.RequiredCapabilities))
	}
}

// TestConsumer_Created_DecodesDestinationLocationRole proves this
// consumer decodes process-path-management's destinationLocationRole
// field (ADR-0009 there) from a REALISTIC ProcessPathCreated payload —
// the exact wire shape that service's kafka publisher emits (see its
// internal/adapters/outbound/kafka/publisher.go), not a hand-simplified
// stand-in — and carries it into the local PathDefinition unchanged.
func TestConsumer_Created_DecodesDestinationLocationRole(t *testing.T) {
	realisticEnvelope := []byte(`{
		"event_id": "6a2d3b8f-0e42-4b7c-9f1d-7c3e2f6b8a91",
		"event_type": "ProcessPathCreated",
		"occurred_at": "2026-09-13T00:00:00Z",
		"source": "process-path-management",
		"data": {
			"path_id": "PACK",
			"match_prefix": "pack",
			"direct": true,
			"required_capabilities": ["pack"],
			"destination_location_role": "Drop",
			"cycle_time_p95": "2h0m0s",
			"eligibility": {}
		}
	}`)
	reader := &fakeReader{messages: []kafkago.Message{{Partition: 0, Offset: 0, Value: realisticEnvelope}}}
	c := newTestConsumer(reader, targetOffsets{0: 1})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() { _ = c.Run(ctx) }()

	if err := c.WaitReady(ctx); err != nil {
		t.Fatalf("expected Ready before timeout, got: %v", err)
	}

	def, err := c.Lookup("pack")
	if err != nil {
		t.Fatalf("expected PACK to resolve, got: %v", err)
	}
	if def.DestinationLocationRole != "Drop" {
		t.Fatalf("expected DestinationLocationRole %q, got %q", "Drop", def.DestinationLocationRole)
	}
}

// TestConsumer_Created_OmittedDestinationLocationRole_IsEmptyString proves
// the common case — a path with no declared destination role, which
// process-path-management omits entirely from the wire payload rather
// than empty-stringing (ADR-0009 there) — decodes to the Go zero value,
// not some other sentinel, so an existing catalogue entry without this
// field keeps behaving exactly as before this change.
func TestConsumer_Created_OmittedDestinationLocationRole_IsEmptyString(t *testing.T) {
	reader := &fakeReader{
		messages: []kafkago.Message{
			envelopeMsg(t, 0, 0, eventTypeCreated, pathData{PathId: "PICK", MatchPrefix: "pick", Direct: true, RequiredCapabilities: []string{"pick"}}),
		},
	}
	c := newTestConsumer(reader, targetOffsets{0: 1})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() { _ = c.Run(ctx) }()

	if err := c.WaitReady(ctx); err != nil {
		t.Fatalf("expected Ready before timeout, got: %v", err)
	}

	def, err := c.Lookup("pick")
	if err != nil {
		t.Fatalf("expected PICK to resolve, got: %v", err)
	}
	if def.DestinationLocationRole != "" {
		t.Fatalf("expected empty DestinationLocationRole for a path with none declared, got %q", def.DestinationLocationRole)
	}
}

func TestConsumer_UnknownEventType_IsIgnored(t *testing.T) {
	reader := &fakeReader{
		messages: []kafkago.Message{
			envelopeMsg(t, 0, 0, "SomeFutureEventType", map[string]any{}),
		},
	}
	c := newTestConsumer(reader, targetOffsets{0: 1})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() { _ = c.Run(ctx) }()

	if err := c.WaitReady(ctx); err != nil {
		t.Fatalf("expected Ready before timeout even for an unrecognized event type, got: %v", err)
	}
}
