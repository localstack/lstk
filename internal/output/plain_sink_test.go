package output

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

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

	assert.Equal(t, "Warning: something went wrong\n", out.String())
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
