package telemetry

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

// Attribute keys added to every log record emitted while a span is active.
const (
	TraceIDKey = "trace_id"
	SpanIDKey  = "span_id"
)

// TraceHandler decorates another slog.Handler, appending trace_id and
// span_id to each record whose context carries a valid span context. Records
// logged outside a span pass through unchanged, so nothing is added to
// startup or shutdown lines.
type TraceHandler struct {
	inner slog.Handler
}

// NewTraceHandler wraps inner so its records are correlated with the active
// span. Wrapping a nil handler returns nil so callers can compose it
// unconditionally.
func NewTraceHandler(inner slog.Handler) *TraceHandler {
	if inner == nil {
		return nil
	}
	return &TraceHandler{inner: inner}
}

func (h *TraceHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *TraceHandler) Handle(ctx context.Context, record slog.Record) error {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		record = record.Clone()
		record.AddAttrs(
			slog.String(TraceIDKey, sc.TraceID().String()),
			slog.String(SpanIDKey, sc.SpanID().String()),
		)
	}
	return h.inner.Handle(ctx, record)
}

func (h *TraceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &TraceHandler{inner: h.inner.WithAttrs(attrs)}
}

func (h *TraceHandler) WithGroup(name string) slog.Handler {
	return &TraceHandler{inner: h.inner.WithGroup(name)}
}
