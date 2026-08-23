package usecases

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
)

// meterName is the instrumentation scope for this service's business
// metrics — the domain events the use cases produce, not HTTP traffic.
const meterName = "github.com/claudioed/wes-work-planning/internal/application/usecases"

// AttrPathId is the process-path attribute every path-scoped business
// metric carries.
const AttrPathId = "path_id"

// newInt64Counter builds a counter from the global meter provider. The OTel
// global provider delegates to the real SDK once telemetry.Setup installs
// it, so counters built before Setup still record; if the instrument cannot
// be created at all, a no-op stands in so instrumentation can never fail a
// use case.
func newInt64Counter(name string, opts ...metric.Int64CounterOption) metric.Int64Counter {
	counter, err := otel.Meter(meterName).Int64Counter(name, opts...)
	if err != nil {
		otel.Handle(err)
		c, _ := noop.NewMeterProvider().Meter(meterName).Int64Counter(name)
		return c
	}
	return counter
}
