package output

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

type failingWriter struct {
	err error
}

func (w *failingWriter) Write(p []byte) (n int, err error) {
	return 0, w.err
}

func TestPlainSink_EmitsMessageEventInfo(t *testing.T) {
	var out bytes.Buffer
	sink := NewPlainSink(&out)

	Emit(sink, MessageEvent{Severity: SeverityInfo, Text: "hello"})

	assert.Equal(t, "hello\n", out.String())
}

func TestPlainSink_EmitsMessageEventWarning(t *testing.T) {
	var out bytes.Buffer
	sink := NewPlainSink(&out)

	Emit(sink, MessageEvent{Severity: SeverityWarning, Text: "something went wrong"})

	assert.Equal(t, "> Warning: something went wrong\n", out.String())
}

func TestPlainSink_EmitsStatusEvent(t *testing.T) {
	tests := []struct {
		name     string
		event    ContainerStatusEvent
		expected string
	}{
		{
			name:     "pulling phase",
			event:    ContainerStatusEvent{Phase: "pulling", Container: "localstack/localstack:latest"},
			expected: "Pulling localstack/localstack:latest...\n",
		},
		{
			name:     "starting phase",
			event:    ContainerStatusEvent{Phase: "starting", Container: "localstack"},
			expected: "Starting localstack...\n",
		},
		{
			name:     "waiting phase",
			event:    ContainerStatusEvent{Phase: "waiting", Container: "localstack"},
			expected: "Waiting for localstack to be ready...\n",
		},
		{
			name:     "ready phase with detail",
			event:    ContainerStatusEvent{Phase: "ready", Container: "localstack", Detail: "abc123"},
			expected: "localstack ready (abc123)\n",
		},
		{
			name:     "ready phase without detail",
			event:    ContainerStatusEvent{Phase: "ready", Container: "localstack"},
			expected: "localstack ready\n",
		},
		{
			name:     "unknown phase with detail",
			event:    ContainerStatusEvent{Phase: "custom", Container: "localstack", Detail: "info"},
			expected: "localstack: custom (info)\n",
		},
		{
			name:     "unknown phase without detail",
			event:    ContainerStatusEvent{Phase: "custom", Container: "localstack"},
			expected: "localstack: custom\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			sink := NewPlainSink(&out)

			Emit(sink, tt.event)

			assert.Equal(t, tt.expected, out.String())
		})
	}
}

func TestPlainSink_EmitsProgressEvent(t *testing.T) {
	tests := []struct {
		name     string
		event    ProgressEvent
		expected string
	}{
		{
			name: "with total (percentage)",
			event: ProgressEvent{
				Container: "localstack",
				LayerID:   "abc123",
				Status:    "Downloading",
				Current:   50,
				Total:     100,
			},
			expected: "  abc123: Downloading 50.0%\n",
		},
		{
			name: "without total (status only)",
			event: ProgressEvent{
				Container: "localstack",
				LayerID:   "abc123",
				Status:    "Pull complete",
				Current:   0,
				Total:     0,
			},
			expected: "  abc123: Pull complete\n",
		},
		{
			name: "no status and no total",
			event: ProgressEvent{
				Container: "localstack",
				LayerID:   "abc123",
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			sink := NewPlainSink(&out)

			Emit(sink, tt.event)

			assert.Equal(t, tt.expected, out.String())
		})
	}
}

func TestPlainSink_EmitsContainerLogLineEvent(t *testing.T) {
	var out bytes.Buffer
	sink := NewPlainSink(&out)

	Emit(sink, ContainerLogLineEvent{Line: "2024-01-01 hello from container"})

	assert.Equal(t, "2024-01-01 hello from container\n", out.String())
}

func TestPlainSink_ErrReturnsNilOnSuccess(t *testing.T) {
	var out bytes.Buffer
	sink := NewPlainSink(&out)

	Emit(sink, MessageEvent{Severity: SeverityInfo, Text: "hello"})

	assert.NoError(t, sink.Err())
}

func TestPlainSink_ErrCapturesWriteError(t *testing.T) {
	writeErr := errors.New("write failed")
	sink := NewPlainSink(&failingWriter{err: writeErr})

	Emit(sink, MessageEvent{Severity: SeverityInfo, Text: "hello"})

	assert.Equal(t, writeErr, sink.Err())
}

func TestPlainSink_ErrStoresOnlyFirstError(t *testing.T) {
	firstErr := errors.New("first error")
	sink := NewPlainSink(&failingWriter{err: firstErr})

	Emit(sink, MessageEvent{Severity: SeverityInfo, Text: "first"})
	Emit(sink, MessageEvent{Severity: SeverityInfo, Text: "second"})

	assert.Equal(t, firstErr, sink.Err())
}

func TestPlainSink_UsesFormatterParity(t *testing.T) {
	t.Parallel()

	events := []any{
		MessageEvent{Severity: SeverityInfo, Text: "hello"},
		MessageEvent{Severity: SeverityWarning, Text: "careful"},
		ContainerStatusEvent{Phase: "starting", Container: "localstack"},
		ProgressEvent{LayerID: "abc", Status: "Downloading", Current: 1, Total: 2},
	}

	for _, event := range events {
		var out bytes.Buffer
		sink := NewPlainSink(&out)

		switch e := event.(type) {
		case MessageEvent:
			Emit(sink, e)
		case ContainerStatusEvent:
			Emit(sink, e)
		case ProgressEvent:
			Emit(sink, e)
		default:
			t.Fatalf("unsupported event type in test: %T", event)
		}

		line, ok := FormatEventLine(event)
		if !ok {
			t.Fatalf("expected formatter output for %T", event)
		}
		if got, want := out.String(), fmt.Sprintf("%s\n", line); got != want {
			t.Fatalf("output mismatch for %T: got=%q want=%q", event, got, want)
		}
	}
}
