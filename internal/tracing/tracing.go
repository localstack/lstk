// Tracing is disabled by default; set LSTK_OTEL=1 to enable it.
// Spans are exported via OTLP/HTTP to localhost:4318 by default.
//
// To start a local trace backend (Jaeger):
//
//	docker compose -f docker-compose.tracing.yaml up -d
//
// Then open http://localhost:16686 to browse traces.
// Configure the exporter via standard OTel env vars (OTEL_EXPORTER_OTLP_ENDPOINT, etc.),
// which the SDK reads automatically.
package tracing

import (
	"context"
	stdruntime "runtime"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/localstack/lstk/internal/log"
	"github.com/localstack/lstk/internal/version"
)

func Init(ctx context.Context, logger log.Logger) func(context.Context) error {
	noop := func(context.Context) error { return nil }

	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		logger.Error("otel error: %v", err)
	}))

	exp, err := otlptracehttp.New(ctx, otlptracehttp.WithInsecure())
	if err != nil {
		logger.Error("failed to initialize otel exporter: %v", err)
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
