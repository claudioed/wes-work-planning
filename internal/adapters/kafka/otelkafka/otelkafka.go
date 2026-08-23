// Package otelkafka carries W3C trace context across the Kafka message
// boundary. kafka-go has no automatic instrumentation, so the outbound
// adapter injects the active span context into a message's headers and the
// inbound adapter extracts it again, making each consumer span a child of
// the producer span that emitted the message. It sits beside envelope,
// shared by the inbound and outbound Kafka adapters without either
// depending on the other.
package otelkafka

import (
	"context"

	kafkago "github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
)

// TracerName identifies the instrumentation scope of the Kafka spans.
const TracerName = "github.com/claudioed/wes-work-planning/internal/adapters/kafka"

// HeaderCarrier adapts a kafka-go header slice to the propagation
// TextMapCarrier interface. It is a pointer receiver because Set appends.
type HeaderCarrier struct {
	Headers *[]kafkago.Header
}

var _ propagation.TextMapCarrier = HeaderCarrier{}

// NewHeaderCarrier returns a carrier over headers.
func NewHeaderCarrier(headers *[]kafkago.Header) HeaderCarrier {
	return HeaderCarrier{Headers: headers}
}

// Get returns the value of the first header with the given key, or "".
func (c HeaderCarrier) Get(key string) string {
	if c.Headers == nil {
		return ""
	}
	for _, h := range *c.Headers {
		if h.Key == key {
			return string(h.Value)
		}
	}
	return ""
}

// Set writes key/value, replacing any existing header with that key so a
// re-injected context never leaves two conflicting traceparent headers.
func (c HeaderCarrier) Set(key, value string) {
	if c.Headers == nil {
		return
	}
	for i, h := range *c.Headers {
		if h.Key == key {
			(*c.Headers)[i].Value = []byte(value)
			return
		}
	}
	*c.Headers = append(*c.Headers, kafkago.Header{Key: key, Value: []byte(value)})
}

// Keys returns every header key, in order.
func (c HeaderCarrier) Keys() []string {
	if c.Headers == nil {
		return nil
	}
	keys := make([]string, 0, len(*c.Headers))
	for _, h := range *c.Headers {
		keys = append(keys, h.Key)
	}
	return keys
}

// Inject writes the trace context in ctx into msg's headers.
func Inject(ctx context.Context, msg *kafkago.Message) {
	otel.GetTextMapPropagator().Inject(ctx, NewHeaderCarrier(&msg.Headers))
}

// Extract returns ctx enriched with the trace context found in msg's
// headers, so a span started from it becomes a child of the producer's span.
func Extract(ctx context.Context, msg *kafkago.Message) context.Context {
	return otel.GetTextMapPropagator().Extract(ctx, NewHeaderCarrier(&msg.Headers))
}

// StartPublishSpan starts the producer-side span for topic, named
// "kafka.publish <topic>" per the OTel messaging semantic conventions.
func StartPublishSpan(ctx context.Context, topic string, extra ...attribute.KeyValue) (context.Context, trace.Span) {
	return start(ctx, "kafka.publish "+topic, "publish", topic, semconv.MessagingOperationTypeSend, extra...)
}

// StartConsumeSpan starts the consumer-side span for topic, named
// "kafka.consume <topic>" per the OTel messaging semantic conventions.
func StartConsumeSpan(ctx context.Context, topic string, extra ...attribute.KeyValue) (context.Context, trace.Span) {
	return start(ctx, "kafka.consume "+topic, "consume", topic, semconv.MessagingOperationTypeProcess, extra...)
}

func start(ctx context.Context, spanName, operationName, topic string, operationType attribute.KeyValue, extra ...attribute.KeyValue) (context.Context, trace.Span) {
	attrs := append([]attribute.KeyValue{
		semconv.MessagingSystemKafka,
		semconv.MessagingOperationName(operationName),
		operationType,
		semconv.MessagingDestinationName(topic),
	}, extra...)

	kind := trace.SpanKindProducer
	if operationName == "consume" {
		kind = trace.SpanKindConsumer
	}

	return otel.Tracer(TracerName).Start(ctx, spanName,
		trace.WithSpanKind(kind),
		trace.WithAttributes(attrs...),
	)
}
