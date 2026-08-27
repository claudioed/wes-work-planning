// Package events provides an EventPublisher that logs and buffers domain
// events, plus a MultiPublisher that fans a single Publish out to several
// publishers. Publisher is an interface so a Kafka-backed implementation can
// later satisfy the same seam.
package events

import (
	"context"
	"errors"

	"github.com/claudioed/wes-work-planning/internal/domain/shared"
)

// MultiPublisher fans one Publish call out to every wrapped publisher, in
// order. It is how the composition root emits each domain event to BOTH the
// integration topic (warehouse.work-planning.events) and the separate
// analytics topic (warehouse.wes.analytics) without either publisher knowing
// about the other (ADR-0011).
//
// It stops at the first publisher that errors and returns that error, so the
// use case sees a publish failure exactly as it would with a single
// publisher; the publishers already delivered are not rolled back (each is an
// independent at-least-once stream, deduplicated downstream on event_id).
type MultiPublisher struct {
	publishers []Publisher
}

// NewMultiPublisher constructs a MultiPublisher over publishers, in the order
// given. A nil publisher in the list is skipped, so a caller can pass an
// optional analytics publisher without a branch.
func NewMultiPublisher(publishers ...Publisher) *MultiPublisher {
	out := make([]Publisher, 0, len(publishers))
	for _, p := range publishers {
		if p != nil {
			out = append(out, p)
		}
	}
	return &MultiPublisher{publishers: out}
}

// Publish forwards events to every wrapped publisher, returning the first
// error encountered.
func (m *MultiPublisher) Publish(ctx context.Context, events ...shared.DomainEvent) error {
	for _, p := range m.publishers {
		if err := p.Publish(ctx, events...); err != nil {
			return err
		}
	}
	return nil
}

// Close closes every wrapped publisher that implements io.Closer-style
// Close() error, joining any errors. Publishers without a Close are skipped.
func (m *MultiPublisher) Close() error {
	var errs []error
	for _, p := range m.publishers {
		if c, ok := p.(interface{ Close() error }); ok {
			if err := c.Close(); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}
