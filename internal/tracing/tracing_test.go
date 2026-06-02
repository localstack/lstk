package tracing

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

func setupPropagation(t *testing.T) {
	t.Helper()
	prevTP := otel.GetTracerProvider()
	prevProp := otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTP)
		otel.SetTextMapPropagator(prevProp)
	})
	otel.SetTracerProvider(sdktrace.NewTracerProvider())
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
}

func TestSubprocessEnvRoundTripsSpanContext(t *testing.T) {
	setupPropagation(t)

	ctx, span := otel.Tracer("test").Start(context.Background(), "parent command")
	defer span.End()

	envEntries := SubprocessEnv(ctx)
	require.NotEmpty(t, envEntries, "expected TRACEPARENT env entry")

	// Simulate the subprocess: look up the injected entries via getenv.
	getenv := func(key string) string {
		for _, e := range envEntries {
			if len(e) > len(key) && e[:len(key)] == key && e[len(key)] == '=' {
				return e[len(key)+1:]
			}
		}
		return ""
	}
	require.NotEmpty(t, getenv("TRACEPARENT"))

	childCtx := ContextWithRemoteParent(context.Background(), getenv)
	remote := trace.SpanContextFromContext(childCtx)
	require.True(t, remote.IsValid(), "extracted span context should be valid")
	assert.Equal(t, span.SpanContext().TraceID(), remote.TraceID(), "subprocess spans must join the parent's trace")
	assert.Equal(t, span.SpanContext().SpanID(), remote.SpanID(), "subprocess spans must parent to the command span")
	assert.True(t, remote.IsRemote())
}

func TestSubprocessEnvEmptyWithoutActiveSpan(t *testing.T) {
	setupPropagation(t)
	assert.Empty(t, SubprocessEnv(context.Background()))
}

func TestContextWithRemoteParentNoEnvIsNoop(t *testing.T) {
	setupPropagation(t)
	ctx := ContextWithRemoteParent(context.Background(), func(string) string { return "" })
	assert.False(t, trace.SpanContextFromContext(ctx).IsValid())
}
