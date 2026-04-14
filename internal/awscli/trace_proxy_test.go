package awscli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestStartTraceProxy_InjectsTraceparentHeader(t *testing.T) {
	// Set up a real TracerProvider + W3C propagator so we get a populated span context.
	tp := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}))
	t.Cleanup(func() {
		otel.SetTracerProvider(otel.GetTracerProvider())
	})

	ctx, span := otel.Tracer("test").Start(context.Background(), "test-span")
	defer span.End()

	// Backend that records the headers it receives.
	var received http.Header
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	proxyURL, stop := startTraceProxy(ctx, backend.URL)
	defer stop()

	resp, err := http.Get(proxyURL + "/test")
	require.NoError(t, err)
	_ = resp.Body.Close()

	assert.NotEmpty(t, received.Get("traceparent"), "proxy should inject traceparent header")
}

func TestStartTraceProxy_NoSpan_ReturnTargetDirectly(t *testing.T) {
	// With no active span the propagator injects nothing, so no proxy is started.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}))

	proxyURL, stop := startTraceProxy(context.Background(), "http://localhost:4566")
	defer stop()

	assert.Equal(t, "http://localhost:4566", proxyURL, "should return target unchanged when no span is active")
}
