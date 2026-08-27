package telemetry_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"github.com/claudioed/wes-work-planning/internal/adapters/outbound/telemetry"
)

// TestLogrBridgeEmitsStructuredDebugRecords covers the adapter that keeps
// the OTel SDK's own diagnostics on slog: they must be JSON, at debug level
// (they are not service errors), and carry their key/value pairs.
func TestLogrBridgeEmitsStructuredDebugRecords(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	log := telemetry.NewLogr(logger).WithName("otlp").WithValues("component", "exporter")

	log.Error(errors.New("connection refused"), "traces export")

	var record map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &record); err != nil {
		t.Fatalf("log line is not JSON: %v (%q)", err, buf.String())
	}

	if record["level"] != "DEBUG" {
		t.Errorf("level = %v, want DEBUG", record["level"])
	}
	if record["msg"] != "traces export" {
		t.Errorf("msg = %v, want %q", record["msg"], "traces export")
	}
	if record["component"] != "exporter" {
		t.Errorf("WithValues attribute lost: %v", record["component"])
	}
	if record["logger"] != "otlp" {
		t.Errorf("logger = %v, want otlp", record["logger"])
	}
	if record["error"] != "connection refused" {
		t.Errorf("error = %v, want connection refused", record["error"])
	}
}

func TestLogrBridgeInfoIsDebugAndNamesNest(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	log := telemetry.NewLogr(logger).WithName("otel").WithName("metric")

	if !log.Enabled() {
		t.Fatal("logr sink reports disabled at debug level")
	}
	log.Info("periodic reader started", "interval", "60s")

	var record map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &record); err != nil {
		t.Fatalf("log line is not JSON: %v (%q)", err, buf.String())
	}
	if record["level"] != "DEBUG" {
		t.Errorf("level = %v, want DEBUG", record["level"])
	}
	if record["logger"] != "otel/metric" {
		t.Errorf("logger = %v, want otel/metric", record["logger"])
	}
	if record["interval"] != "60s" {
		t.Errorf("interval = %v, want 60s", record["interval"])
	}
}

// TestLogrBridgeSilentAboveDebug proves SDK chatter disappears entirely at
// the default LOG_LEVEL=info.
func TestLogrBridgeSilentAboveDebug(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	log := telemetry.NewLogr(logger)

	if log.Enabled() {
		t.Error("logr sink reports enabled with an info-level logger")
	}
	log.Info("noise")
	log.Error(errors.New("boom"), "more noise")

	if buf.Len() != 0 {
		t.Errorf("SDK diagnostics leaked at info level: %q", buf.String())
	}
}
