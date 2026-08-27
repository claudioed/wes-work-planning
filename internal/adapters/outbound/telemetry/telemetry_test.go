package telemetry_test

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/claudioed/wes-work-planning/internal/adapters/outbound/telemetry"
)

// TestSetupWithoutCollectorDoesNotBlock is the executable form of the
// "a missing Collector degrades to dropped telemetry, never to a service
// that won't start" requirement: nothing is listening on port 1, yet Setup
// must return promptly and without error, spans must still be startable,
// and shutdown must honour its context deadline instead of hanging. The
// final flush is allowed to fail — there is nowhere to flush to — which is
// why main logs that error rather than treating it as fatal.
func TestSetupWithoutCollectorDoesNotBlock(t *testing.T) {
	type result struct {
		setupTook time.Duration
		err       error
	}
	done := make(chan result, 1)

	go func() {
		start := time.Now()
		shutdown, err := telemetry.Setup(context.Background(), "wes-work-planning", "test", "127.0.0.1:1")
		took := time.Since(start)
		if err != nil {
			done <- result{took, err}
			return
		}

		// Emit a span and let the batch processor try (and fail) to export it.
		_, span := otel.Tracer("test").Start(context.Background(), "unreachable-collector")
		span.End()

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = shutdown(ctx)

		done <- result{took, nil}
	}()

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("Setup against an unreachable collector: %v", got.err)
		}
		if got.setupTook > 2*time.Second {
			t.Fatalf("Setup took %s with no collector listening; the exporter must dial lazily", got.setupTook)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("Setup or shutdown blocked with no collector listening")
	}
}

func TestDefaultEndpointIsTheCollectorGRPCPort(t *testing.T) {
	if telemetry.DefaultEndpoint != "localhost:4317" {
		t.Fatalf("got default endpoint %q, want the collector's standard OTLP/gRPC port", telemetry.DefaultEndpoint)
	}
}

func TestNewResourceCarriesServiceIdentity(t *testing.T) {
	res, err := telemetry.NewResource("wes-work-planning", "1.2.3", "staging")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := map[attribute.Key]string{
		"service.name":                "wes-work-planning",
		"service.version":             "1.2.3",
		"deployment.environment.name": "staging",
	}

	got := map[attribute.Key]string{}
	for _, kv := range res.Attributes() {
		got[kv.Key] = kv.Value.String()
	}

	for key, wantValue := range want {
		if got[key] != wantValue {
			t.Errorf("resource attribute %s = %q, want %q", key, got[key], wantValue)
		}
	}
}

// TestSetupAcceptsBothEndpointForms guards the tolerance for the OTLP-spec
// URL form and the bare host:port form the fleet's Helm values and this
// package's default use.
func TestSetupAcceptsBothEndpointForms(t *testing.T) {
	for _, endpoint := range []string{"127.0.0.1:1", "http://127.0.0.1:1", "https://127.0.0.1:1"} {
		t.Run(endpoint, func(t *testing.T) {
			shutdown, err := telemetry.Setup(context.Background(), "wes-work-planning", "test", endpoint)
			if err != nil {
				t.Fatalf("Setup(%q): %v", endpoint, err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = shutdown(ctx)
		})
	}
}
