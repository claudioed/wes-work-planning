package http_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	inboundhttp "github.com/claudioed/wes-work-planning/internal/adapters/inbound/http"
	"github.com/claudioed/wes-work-planning/internal/adapters/outbound/events"
	"github.com/claudioed/wes-work-planning/internal/adapters/outbound/memory"
	"github.com/claudioed/wes-work-planning/internal/adapters/outbound/telemetry"
	"github.com/claudioed/wes-work-planning/internal/application/usecases"
)

// newTracedRouter builds the router the way main does — real tracer
// provider, JSON logger behind the trace handler — but exports spans into
// an in-memory recorder instead of OTLP, so this stays hermetic.
func newTracedRouter(t *testing.T, logs *bytes.Buffer) (http.Handler, *tracetest.InMemoryExporter) {
	t.Helper()

	exporter := tracetest.NewInMemoryExporter()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(tracerProvider)
	t.Cleanup(func() { _ = tracerProvider.Shutdown(t.Context()) })

	pools := memory.NewWorkPoolRepo()
	workUnits := memory.NewWorkUnitRepo()
	publisher := events.NewLogPublisher(nil)
	clock := memory.FixedClock{At: time.Date(2026, 8, 21, 8, 0, 0, 0, time.UTC)}

	h := &inboundhttp.Handlers{
		EnqueueWorkUnit: usecases.NewEnqueueWorkUnit(workUnits, pools, publisher, clock),
		ReleaseNextWork: usecases.NewReleaseNextWork(pools, workUnits, publisher, clock),
		SampleBacklog:   usecases.NewSampleBacklog(pools, publisher, clock),
	}

	handler := slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(telemetry.NewTraceHandler(handler))

	return inboundhttp.NewRouter(h, "wes-work-planning", logger), exporter
}

// TestRequestProducesSpanAndCorrelatedLog covers both halves of the
// observability contract at the HTTP edge: otelchi opens a server span named
// after the ROUTE PATTERN (never the raw path, which would make span names
// unbounded), and the access log emitted inside that span carries its
// trace_id/span_id.
func TestRequestProducesSpanAndCorrelatedLog(t *testing.T) {
	var logs bytes.Buffer
	router, exporter := newTracedRouter(t, &logs)

	// Seed a work pool so the telemetry read model has something to report.
	seed := httptest.NewRequest(http.MethodPost, "/paths/pick-a/work-units",
		bytes.NewReader([]byte(`{"workUnitId":"wu-1","cpt":"2026-08-21T10:00:00Z","reference":"ref-1"}`)))
	seed.Header.Set("Content-Type", "application/json")
	seedRec := httptest.NewRecorder()
	router.ServeHTTP(seedRec, seed)
	if seedRec.Code != http.StatusCreated {
		t.Fatalf("seeding the work pool: got status %d, want 201: %s", seedRec.Code, seedRec.Body.String())
	}
	exporter.Reset()
	logs.Reset()

	req := httptest.NewRequest(http.MethodGet, "/paths/pick-a/telemetry", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200: %s", rec.Code, rec.Body.String())
	}

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want exactly 1: %v", len(spans), spans)
	}
	span := spans[0]

	if span.Name != "/paths/{pathId}/telemetry" {
		t.Errorf("span name = %q, want the low-cardinality route pattern", span.Name)
	}

	record := lastLogRecord(t, &logs)
	if record["msg"] != "http request" {
		t.Fatalf("last log record is not the access log: %v", record)
	}
	if got := record[telemetry.TraceIDKey]; got != span.SpanContext.TraceID().String() {
		t.Errorf("log %s = %v, want the span's %s", telemetry.TraceIDKey, got, span.SpanContext.TraceID())
	}
	if got := record[telemetry.SpanIDKey]; got != span.SpanContext.SpanID().String() {
		t.Errorf("log %s = %v, want the span's %s", telemetry.SpanIDKey, got, span.SpanContext.SpanID())
	}
	if got := record["route"]; got != "/paths/{pathId}/telemetry" {
		t.Errorf("log route = %v, want the route pattern", got)
	}
	if got := record["status"]; got != float64(http.StatusOK) {
		t.Errorf("log status = %v, want 200", got)
	}
}

func lastLogRecord(t *testing.T, logs *bytes.Buffer) map[string]any {
	t.Helper()
	lines := bytes.Split(bytes.TrimSpace(logs.Bytes()), []byte("\n"))
	if len(lines) == 0 || len(lines[0]) == 0 {
		t.Fatal("no log output captured")
	}

	var record map[string]any
	if err := json.Unmarshal(lines[len(lines)-1], &record); err != nil {
		t.Fatalf("log line is not JSON: %v (%q)", err, logs.String())
	}
	return record
}
