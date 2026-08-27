package events_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/claudioed/wes-work-planning/internal/adapters/outbound/events"
	"github.com/claudioed/wes-work-planning/internal/domain/shared"
)

// recordingPublisher records the events it was handed and can be made to fail.
type recordingPublisher struct {
	got    []shared.DomainEvent
	err    error
	closed bool
}

func (p *recordingPublisher) Publish(_ context.Context, evts ...shared.DomainEvent) error {
	if p.err != nil {
		return p.err
	}
	p.got = append(p.got, evts...)
	return nil
}

func (p *recordingPublisher) Close() error {
	p.closed = true
	return nil
}

func sampleEvent() shared.DomainEvent {
	pathId, _ := shared.NewPathId("pick-zone-a")
	return shared.NewWorkReleased("wu-1", pathId, time.Now())
}

func TestMultiPublisher_FansOutToAll(t *testing.T) {
	a := &recordingPublisher{}
	b := &recordingPublisher{}
	m := events.NewMultiPublisher(a, nil, b) // nil publisher must be skipped

	ev := sampleEvent()
	if err := m.Publish(context.Background(), ev); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if len(a.got) != 1 || len(b.got) != 1 {
		t.Fatalf("fan-out failed: a=%d b=%d, want 1 each", len(a.got), len(b.got))
	}
}

func TestMultiPublisher_StopsAtFirstError(t *testing.T) {
	wantErr := errors.New("boom")
	a := &recordingPublisher{err: wantErr}
	b := &recordingPublisher{}
	m := events.NewMultiPublisher(a, b)

	if err := m.Publish(context.Background(), sampleEvent()); !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
	if len(b.got) != 0 {
		t.Errorf("second publisher should not have been called after the first errored")
	}
}

func TestMultiPublisher_CloseClosesAll(t *testing.T) {
	a := &recordingPublisher{}
	b := &recordingPublisher{}
	m := events.NewMultiPublisher(a, b)

	if err := m.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !a.closed || !b.closed {
		t.Errorf("not all publishers closed: a=%v b=%v", a.closed, b.closed)
	}
}
