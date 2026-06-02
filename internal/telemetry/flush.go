package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/localstack/lstk/internal/tracing"
	"github.com/localstack/lstk/internal/version"
)

// flushTimeout caps the flusher's lifetime so orphans can't linger if the endpoint hangs.
const flushTimeout = 10 * time.Second

// FlushCommandName is the argv[1] sentinel for the detached flusher subprocess.
// cmd.Execute must short-circuit on it before constructing a telemetry client,
// otherwise the flusher would emit events and spawn flushers recursively.
const FlushCommandName = "__flush-telemetry"

func userAgent() string {
	return fmt.Sprintf("localstack lstk/%s (%s; %s)", version.Version(), runtime.GOOS, runtime.GOARCH)
}

// RunFlush reads JSON-line eventBody values from `in` and POSTs each one to
// `endpoint`. It runs in the detached subprocess, bounded by flushTimeout.
func RunFlush(ctx context.Context, endpoint string, in io.Reader) error {
	ctx, cancel := context.WithTimeout(ctx, flushTimeout)
	defer cancel()

	ctx, span := otel.Tracer("github.com/localstack/lstk/internal/telemetry").Start(ctx, "telemetry flush")
	defer span.End()

	client := &http.Client{
		Timeout: 3 * time.Second,
		Transport: otelhttp.NewTransport(
			http.DefaultTransport,
			otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
				return "telemetry " + r.Method + " " + r.URL.Path
			}),
		),
	}

	count := 0
	dec := json.NewDecoder(in)
	for dec.More() {
		if ctx.Err() != nil {
			break
		}
		var ev eventBody
		if err := dec.Decode(&ev); err != nil {
			span.RecordError(err)
			span.SetAttributes(attribute.Int("telemetry.events", count))
			return err
		}
		postEvent(ctx, client, endpoint, ev)
		count++
	}
	span.SetAttributes(attribute.Int("telemetry.events", count))
	return nil
}

func postEvent(ctx context.Context, client *http.Client, endpoint string, ev eventBody) {
	data, err := json.Marshal(requestBody{Events: []eventBody{ev}})
	if err != nil {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent())

	resp, err := client.Do(req)
	if err != nil {
		return
	}
	_ = resp.Body.Close()
}

// NewWithInProcessFlush returns an enabled Client whose Close flushes synchronously
// in-process, so tests can verify event delivery without forking the binary.
func NewWithInProcessFlush(endpoint string) *Client {
	c := New(endpoint, false)
	c.flushFn = func(ctx context.Context, endpoint string, events []eventBody) {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		for _, ev := range events {
			_ = enc.Encode(ev)
		}
		_ = RunFlush(ctx, endpoint, &buf)
	}
	return c
}

// spawnDetachedFlusher launches the current binary as a detached subprocess and
// streams events to its stdin as JSON lines, without waiting for it to finish.
func spawnDetachedFlusher(ctx context.Context, endpoint string, events []eventBody) {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command(exe, FlushCommandName, "--endpoint", endpoint)
	cmd.SysProcAttr = detachedSysProcAttr()
	if traceEnv := tracing.SubprocessEnv(ctx); len(traceEnv) > 0 {
		cmd.Env = append(os.Environ(), traceEnv...)
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return
	}

	enc := json.NewEncoder(stdin)
	for _, ev := range events {
		if err := enc.Encode(ev); err != nil {
			break
		}
	}
	_ = stdin.Close()
	_ = cmd.Process.Release()
}
