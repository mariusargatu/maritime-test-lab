// Package tracing wires the OpenTelemetry SDK: an OTLP/gRPC exporter to the
// collector, a service-named resource, AlwaysOn sampling (lab traffic is tiny),
// and the W3C tracecontext propagator. Init is a no-op when the endpoint is
// empty, so unit/integration tests run untraced without extra config.
package tracing

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Init installs the global tracer provider and propagator. endpoint is the OTLP
// gRPC target (e.g. "otel-collector:4317"); empty means tracing is disabled but
// context still propagates (no-op spans). Returns a shutdown func to flush spans.
func Init(ctx context.Context, serviceName, endpoint string) (func(context.Context) error, error) {
	// The propagator is always set so trace context flows across gRPC/Kafka even
	// when this process exports nothing.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	if endpoint == "" {
		return func(context.Context) error { return nil }, nil
	}

	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, fmt.Errorf("tracing: otlp exporter: %w", err)
	}

	res, err := resource.New(ctx, resource.WithAttributes(attribute.String("service.name", serviceName)))
	if err != nil {
		return nil, fmt.Errorf("tracing: resource: %w", err)
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(provider)
	return provider.Shutdown, nil
}
