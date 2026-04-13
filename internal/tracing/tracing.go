// Package tracing configures OpenTelemetry distributed tracing for lstk.
// Spans are exported via OTLP/HTTP to localhost:4318 by default.
//
// To start a local trace backend (Jaeger):
//
//	docker compose -f docker-compose.tracing.yaml up -d
//
// Then open http://localhost:16686 to browse traces.
// Override the endpoint with OTEL_EXPORTER_OTLP_ENDPOINT (e.g. "http://localhost:4318").
// Export errors (e.g. no collector running) are silently ignored.
package tracing

import (
	"context"
	stdruntime "runtime"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/localstack/lstk/internal/version"
)

// Init configures the global OpenTelemetry TracerProvider and returns a shutdown
// function that must be called before process exit to flush pending spans.
// If initialisation fails, a no-op shutdown is returned.
func Init(ctx context.Context, otlpEndpoint string) func(context.Context) error {
	noop := func(context.Context) error { return nil }

	// Suppress export errors (e.g. "connection refused" when no collector is running).
	// Tracing is best-effort: lstk works normally without a running collector.
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(error) {}))

	// Default to plain HTTP for local collectors (Jaeger, OTel Collector on localhost).
	// Use TLS when the endpoint is an https:// URL.
	var exporterOpts []otlptracehttp.Option
	exporterOpts = append(exporterOpts, otlptracehttp.WithEndpointURL(otlpEndpoint))
	if !strings.HasPrefix(otlpEndpoint, "https://") {
		exporterOpts = append(exporterOpts, otlptracehttp.WithInsecure())
	}
	exp, err := otlptracehttp.New(ctx, exporterOpts...)
	if err != nil {
		return noop
	}

	res := resource.NewWithAttributes("",
		attribute.String("service.name", "lstk"),
		attribute.String("service.version", version.Version()),
		attribute.String("os.type", stdruntime.GOOS),
		attribute.String("host.arch", stdruntime.GOARCH),
	)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return tp.Shutdown
}
