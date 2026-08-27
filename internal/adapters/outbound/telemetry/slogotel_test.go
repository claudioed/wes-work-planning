package telemetry_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/claudioed/wes-work-planning/internal/adapters/outbound/telemetry"
)

// newCapturingLogger returns a logger writing JSON into buf through the
// trace handler, mirroring how main composes the process logger.
func newCapturingLogger(buf *bytes.Buffer) *slog.Logger {
	handler := slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	return slog.New(telemetry.NewTraceHandler(handler))
}

func decodeRecord(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var record map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &record); err != nil {
		t.Fatalf("log line is not JSON: %v (%q)", err, buf.String())
	}
	return record
}

func TestTraceHandlerAddsTraceAndSpanIDInsideASpan(t *testing.T) {
	var buf bytes.Buffer
	logger := newCapturingLogger(&buf)

	tracerProvider := sdktrace.NewTracerProvider()
	t.Cleanup(func() { _ = tracerProvider.Shutdown(context.Background()) })

	ctx, span := tracerProvider.Tracer("test").Start(context.Background(), "handling request")
	defer span.End()

	logger.InfoContext(ctx, "http request", "status", 200)

	record := decodeRecord(t, &buf)
	sc := span.SpanContext()

	if got := record[telemetry.TraceIDKey]; got != sc.TraceID().String() {
		t.Errorf("%s = %v, want %s", telemetry.TraceIDKey, got, sc.TraceID())
	}
	if got := record[telemetry.SpanIDKey]; got != sc.SpanID().String() {
		t.Errorf("%s = %v, want %s", telemetry.SpanIDKey, got, sc.SpanID())
	}
	if got := record["status"]; got != float64(200) {
		t.Errorf("the wrapped handler dropped an attribute: status = %v", got)
	}
}

func TestTraceHandlerOmitsIDsOutsideASpan(t *testing.T) {
	var buf bytes.Buffer
	logger := newCapturingLogger(&buf)

	logger.InfoContext(context.Background(), "http server listening", "addr", ":8080")

	record := decodeRecord(t, &buf)
	if _, ok := record[telemetry.TraceIDKey]; ok {
		t.Errorf("%s must be absent with no active span, got %v", telemetry.TraceIDKey, record[telemetry.TraceIDKey])
	}
	if _, ok := record[telemetry.SpanIDKey]; ok {
		t.Errorf("%s must be absent with no active span, got %v", telemetry.SpanIDKey, record[telemetry.SpanIDKey])
	}
	if got := record["addr"]; got != ":8080" {
		t.Errorf("the wrapped handler dropped an attribute: addr = %v", got)
	}
}

// TestTraceHandlerPreservesWithAttrsAndWithGroup guards the two delegating
// methods slog uses when a caller derives a child logger.
func TestTraceHandlerPreservesWithAttrsAndWithGroup(t *testing.T) {
	var buf bytes.Buffer
	logger := newCapturingLogger(&buf).With("component", "kafka").WithGroup("event")

	tracerProvider := sdktrace.NewTracerProvider()
	t.Cleanup(func() { _ = tracerProvider.Shutdown(context.Background()) })

	ctx, span := tracerProvider.Tracer("test").Start(context.Background(), "kafka.consume")
	defer span.End()

	logger.InfoContext(ctx, "consumed", "type", "TaskCompleted")

	record := decodeRecord(t, &buf)
	if record["component"] != "kafka" {
		t.Errorf("WithAttrs lost its attribute: %v", record["component"])
	}
	group, ok := record["event"].(map[string]any)
	if !ok {
		t.Fatalf("WithGroup lost its group: %v", record["event"])
	}
	if group["type"] != "TaskCompleted" {
		t.Errorf("grouped attribute = %v, want TaskCompleted", group["type"])
	}
	// trace_id is added by the wrapper itself, so it lands inside the group.
	if group[telemetry.TraceIDKey] != span.SpanContext().TraceID().String() {
		t.Errorf("%s = %v, want %s", telemetry.TraceIDKey, group[telemetry.TraceIDKey], span.SpanContext().TraceID())
	}
}

func TestNewTraceHandlerOfNilIsNil(t *testing.T) {
	if h := telemetry.NewTraceHandler(nil); h != nil {
		t.Fatalf("got %v, want nil so callers can compose unconditionally", h)
	}
}
