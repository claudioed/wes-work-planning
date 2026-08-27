// Package telemetry is the outbound OpenTelemetry adapter: it builds the
// process-wide tracer and meter providers, exports both over OTLP/gRPC to a
// Collector, wires Go runtime metrics, and provides the slog handler that
// correlates log records with the active span. Only the composition root
// (cmd/wes) depends on it.
package telemetry

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"os"
	"strings"

	runtimemetrics "go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
)

const (
	// DefaultEndpoint is the OTel Collector's standard OTLP/gRPC receiver.
	DefaultEndpoint = "localhost:4317"
	// DefaultServiceVersion is used when SERVICE_VERSION is unset.
	DefaultServiceVersion = "dev"
	// DefaultEnvironment is used when ENVIRONMENT is unset.
	DefaultEnvironment = "local"
)

// Setup builds and installs the global TracerProvider and MeterProvider,
// both exporting over OTLP/gRPC to otlpEndpoint (DefaultEndpoint when
// empty), installs the W3C trace-context propagator, and starts Go runtime
// metrics. deployment.environment is read from the ENVIRONMENT env var
// (default DefaultEnvironment).
//
// The exporters dial lazily and never block: with no Collector listening,
// spans and metrics are dropped and export errors are reported through the
// OTel error handler at debug level, so a missing Collector can never stop
// the service from starting or make a request hang.
//
// The returned func flushes and closes both providers; call it from the
// graceful-shutdown path.
func Setup(ctx context.Context, serviceName, serviceVersion, otlpEndpoint string) (func(context.Context) error, error) {
	if otlpEndpoint == "" {
		otlpEndpoint = DefaultEndpoint
	}

	res, err := NewResource(serviceName, serviceVersion, getenv("ENVIRONMENT", DefaultEnvironment))
	if err != nil {
		return nil, err
	}

	// The SDK's own diagnostics go through slog too, so nothing escapes as
	// plain text on stderr. Set before the exporters, which log while they
	// read their configuration.
	otel.SetLogger(NewLogr(slog.Default()))

	traceExporter, err := otlptracegrpc.New(ctx, append(
		[]otlptracegrpc.Option{otlptracegrpc.WithInsecure()},
		traceEndpointOption(otlpEndpoint),
	)...)
	if err != nil {
		return nil, err
	}

	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(res),
	)

	metricExporter, err := otlpmetricgrpc.New(ctx, append(
		[]otlpmetricgrpc.Option{otlpmetricgrpc.WithInsecure()},
		metricEndpointOption(otlpEndpoint),
	)...)
	if err != nil {
		return nil, errors.Join(err, tracerProvider.Shutdown(ctx))
	}

	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter)),
	)

	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		slog.Debug("opentelemetry error", "error", err)
	}))
	otel.SetTracerProvider(tracerProvider)
	otel.SetMeterProvider(meterProvider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	if err := runtimemetrics.Start(runtimemetrics.WithMeterProvider(meterProvider)); err != nil {
		// Runtime metrics are best-effort: losing them must not stop startup.
		slog.Warn("runtime metrics not started", "error", err)
	}

	return func(ctx context.Context) error {
		return errors.Join(tracerProvider.Shutdown(ctx), meterProvider.Shutdown(ctx))
	}, nil
}

// isEndpointURL reports whether endpoint is a full URL
// ("http://collector:4317") rather than the bare "host:port" form. The OTLP
// spec defines OTEL_EXPORTER_OTLP_ENDPOINT as a URL, but the fleet's Helm
// values and this package's default use host:port, so both are accepted.
func isEndpointURL(endpoint string) bool {
	if !strings.Contains(endpoint, "://") {
		return false
	}
	u, err := url.Parse(endpoint)
	return err == nil && u.Scheme != "" && u.Host != ""
}

func traceEndpointOption(endpoint string) otlptracegrpc.Option {
	if isEndpointURL(endpoint) {
		return otlptracegrpc.WithEndpointURL(endpoint)
	}
	return otlptracegrpc.WithEndpoint(endpoint)
}

func metricEndpointOption(endpoint string) otlpmetricgrpc.Option {
	if isEndpointURL(endpoint) {
		return otlpmetricgrpc.WithEndpointURL(endpoint)
	}
	return otlpmetricgrpc.WithEndpoint(endpoint)
}

// NewResource builds the resource shared by traces and metrics, carrying the
// service.name / service.version / deployment.environment.name semantic
// attributes on top of the SDK defaults.
func NewResource(serviceName, serviceVersion, environment string) (*resource.Resource, error) {
	return resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion(serviceVersion),
			semconv.DeploymentEnvironmentNameKey.String(environment),
		),
	)
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
