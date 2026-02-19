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

func TestPlainSink_EmitsLogEvent(t *testing.T) {
	var out bytes.Buffer
	sink := NewPlainSink(&out)

	Emit(sink, LogEvent{Message: "hello"})

	assert.Equal(t, "hello\n", out.String())
}

func TestPlainSink_EmitsWarningEvent(t *testing.T) {
	var out bytes.Buffer
	sink := NewPlainSink(&out)

	Emit(sink, WarningEvent{Message: "something went wrong"})

	assert.Contains(t, out.String(), "Warning: something went wrong")
}

func TestPlainSink_EmitsSuccessEvent(t *testing.T) {
	var out bytes.Buffer
	sink := NewPlainSink(&out)

	Emit(sink, SuccessEvent{Message: "done"})

	assert.Contains(t, out.String(), "Success: done")
}

func TestPlainSink_EmitsNoteEvent(t *testing.T) {
	var out bytes.Buffer
	sink := NewPlainSink(&out)

	Emit(sink, NoteEvent{Message: "info"})

	assert.Contains(t, out.String(), "Note: info")
}

func TestPlainSink_EmitsContainerStatusEvent(t *testing.T) {
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

	Emit(sink, LogEvent{Message: "hello"})

	assert.NoError(t, sink.Err())
}

func TestPlainSink_ErrCapturesWriteError(t *testing.T) {
	writeErr := errors.New("write failed")
	sink := NewPlainSink(&failingWriter{err: writeErr})

	Emit(sink, LogEvent{Message: "hello"})

	assert.Equal(t, writeErr, sink.Err())
}

func TestPlainSink_ErrStoresOnlyFirstError(t *testing.T) {
	firstErr := errors.New("first error")
	sink := NewPlainSink(&failingWriter{err: firstErr})

	Emit(sink, LogEvent{Message: "first"})
	Emit(sink, LogEvent{Message: "second"})

	assert.Equal(t, firstErr, sink.Err())
}

func TestPlainSink_UsesFormatterParity(t *testing.T) {
	t.Parallel()

	events := []any{
		LogEvent{Message: "hello"},
		ContainerStatusEvent{Phase: "starting", Container: "localstack"},
		ProgressEvent{LayerID: "abc", Status: "Downloading", Current: 1, Total: 2},
		SuccessEvent{Message: "done"},
		NoteEvent{Message: "info"},
		WarningEvent{Message: "something went wrong"},
	}

	for _, event := range events {
		var out bytes.Buffer
		sink := NewPlainSink(&out)

		switch e := event.(type) {
		case LogEvent:
			Emit(sink, e)
		case ContainerStatusEvent:
			Emit(sink, e)
		case ProgressEvent:
			Emit(sink, e)
		case SuccessEvent:
			Emit(sink, e)
		case NoteEvent:
			Emit(sink, e)
		case WarningEvent:
			Emit(sink, e)
		default:
			t.Fatalf("unsupported event type in test: %T", event)
		}

		line, ok := FormatEventLine(event)
		if !ok {
			t.Fatalf("expected formatter output for %T", event)
		}

		got := out.String()
		if !assert.Contains(t, got, line) {
			t.Fatalf("output for %T should contain formatted line: got=%q want to contain=%q", event, got, line)
		}

		switch event.(type) {
		case SuccessEvent, NoteEvent, WarningEvent:
			if !assert.Contains(t, got, "> ") {
				t.Fatalf("output for %T should contain prefix: got=%q", event, got)
			}
		default:
			if got != fmt.Sprintf("%s\n", line) {
				t.Fatalf("output mismatch for %T: got=%q want=%q", event, got, fmt.Sprintf("%s\n", line))
			}
		}
	}
}
