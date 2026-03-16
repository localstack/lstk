package output

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

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
			expected: "Preparing LocalStack...\n",
		},
		{
			name:     "starting phase",
			event:    ContainerStatusEvent{Phase: "starting", Container: "localstack-aws"},
			expected: "Starting LocalStack...\n",
		},
		{
			name:     "waiting phase",
			event:    ContainerStatusEvent{Phase: "waiting", Container: "localstack-aws"},
			expected: "Waiting for LocalStack to be ready...\n",
		},
		{
			name:     "ready phase with detail",
			event:    ContainerStatusEvent{Phase: "ready", Container: "localstack-aws", Detail: "abc123"},
			expected: "LocalStack ready (abc123)\n",
		},
		{
			name:     "ready phase without detail",
			event:    ContainerStatusEvent{Phase: "ready", Container: "localstack-aws"},
			expected: "LocalStack ready\n",
		},
		{
			name:     "unknown phase with detail",
			event:    ContainerStatusEvent{Phase: "custom", Container: "localstack-aws", Detail: "info"},
			expected: "LocalStack: custom (info)\n",
		},
		{
			name:     "unknown phase without detail",
			event:    ContainerStatusEvent{Phase: "custom", Container: "localstack-aws"},
			expected: "LocalStack: custom\n",
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

func TestPlainSink_SuppressesProgressEvent(t *testing.T) {
	var out bytes.Buffer
	sink := NewPlainSink(&out)

	Emit(sink, ProgressEvent{
		Container: "localstack",
		LayerID:   "abc123",
		Status:    "Downloading",
		Current:   50,
		Total:     100,
	})

	assert.Equal(t, "", out.String())
}

func TestPlainSink_EmitsLogLineEvent(t *testing.T) {
	var out bytes.Buffer
	sink := NewPlainSink(&out)

	Emit(sink, LogLineEvent{Source: "container", Line: "2024-01-01 hello from container"})

	assert.Equal(t, "2024-01-01 hello from container\n", out.String())
}

func TestPlainSink_EmitsSpinnerEvent(t *testing.T) {
	t.Run("active spinner", func(t *testing.T) {
		var out bytes.Buffer
		sink := NewPlainSink(&out)

		Emit(sink, SpinnerEvent{Active: true, Text: "Loading"})

		assert.Equal(t, "Loading...\n", out.String())
	})

	t.Run("stop spinner is silent", func(t *testing.T) {
		var out bytes.Buffer
		sink := NewPlainSink(&out)

		Emit(sink, SpinnerEvent{Active: false})

		assert.Equal(t, "", out.String())
	})
}

func TestPlainSink_EmitsErrorEvent(t *testing.T) {
	var out bytes.Buffer
	sink := NewPlainSink(&out)

	Emit(sink, ErrorEvent{
		Title:   "Connection failed",
		Summary: "Cannot connect to Docker",
		Actions: []ErrorAction{{Label: "Start Docker:", Value: "open -a Docker"}},
	})

	expected := "Error: Connection failed\n  Cannot connect to Docker\n  ==> Start Docker: open -a Docker\n"
	assert.Equal(t, expected, out.String())
}

func TestPlainSink_EmitsInstanceInfoEvent(t *testing.T) {
	t.Run("full", func(t *testing.T) {
		var out bytes.Buffer
		sink := NewPlainSink(&out)

		Emit(sink, InstanceInfoEvent{
			EmulatorName:  "LocalStack AWS Emulator",
			Version:       "4.14.1",
			Host:          "localhost.localstack.cloud:4566",
			ContainerName: "localstack-aws",
			Uptime:        4*time.Minute + 23*time.Second,
		})

		expected := "✓ LocalStack AWS Emulator is running (localhost.localstack.cloud:4566)\n  UPTIME: 4m 23s · CONTAINER: localstack-aws · VERSION: 4.14.1\n"
		assert.Equal(t, expected, out.String())
		assert.NoError(t, sink.Err())
	})

	t.Run("minimal", func(t *testing.T) {
		var out bytes.Buffer
		sink := NewPlainSink(&out)

		Emit(sink, InstanceInfoEvent{
			EmulatorName: "LocalStack AWS Emulator",
			Host:         "127.0.0.1:4566",
		})

		expected := "✓ LocalStack AWS Emulator is running (127.0.0.1:4566)\n"
		assert.Equal(t, expected, out.String())
		assert.NoError(t, sink.Err())
	})

	t.Run("write error", func(t *testing.T) {
		writeErr := errors.New("write failed")
		sink := NewPlainSink(&failingWriter{err: writeErr})

		Emit(sink, InstanceInfoEvent{
			EmulatorName: "LocalStack AWS Emulator",
			Host:         "127.0.0.1:4566",
		})

		assert.Equal(t, writeErr, sink.Err())
	})
}

func TestPlainSink_EmitsTableEvent(t *testing.T) {
	t.Run("with rows", func(t *testing.T) {
		var out bytes.Buffer
		sink := NewPlainSink(&out)

		Emit(sink, TableEvent{
			Headers: []string{"SERVICE", "RESOURCE", "REGION", "ACCOUNT"},
			Rows: [][]string{
				{"Lambda", "handler", "us-east-1", "000000000000"},
				{"S3", "my-bucket", "us-east-1", "000000000000"},
			},
		})

		got := out.String()
		assert.Contains(t, got, "SERVICE")
		assert.Contains(t, got, "Lambda")
		assert.Contains(t, got, "S3")
		assert.Contains(t, got, "my-bucket")
		assert.True(t, strings.HasSuffix(got, "\n"))
		assert.NoError(t, sink.Err())
	})

	t.Run("empty rows suppressed", func(t *testing.T) {
		var out bytes.Buffer
		sink := NewPlainSink(&out)

		Emit(sink, TableEvent{Headers: []string{"A"}, Rows: [][]string{}})

		assert.Equal(t, "", out.String())
		assert.NoError(t, sink.Err())
	})

	t.Run("write error", func(t *testing.T) {
		writeErr := errors.New("write failed")
		sink := NewPlainSink(&failingWriter{err: writeErr})

		Emit(sink, TableEvent{
			Headers: []string{"A"},
			Rows:    [][]string{{"val"}},
		})

		assert.Equal(t, writeErr, sink.Err())
	})
}

func TestPlainSink_TableWidth(t *testing.T) {
	t.Parallel()

	tableEvent := TableEvent{
		Headers: []string{"SERVICE", "RESOURCE", "REGION", "ACCOUNT"},
		Rows: [][]string{
			{"CloudFormation", "8245db0d-5c05-4209-90f0-51ec48446a58", "us-east-1", "000000000000"},
			{"EC2", "subnet-816649cee2efc65ac", "eu-central-1", "000000000000"},
			{"Lambda", "HelloWorldFunctionJavaScript", "us-east-1", "000000000000"},
		},
	}

	t.Run("narrow terminal truncates via formatTableWidth", func(t *testing.T) {
		t.Parallel()
		got := formatTableWidth(tableEvent, 80)
		var out bytes.Buffer
		sink := NewPlainSink(&out)
		Emit(sink, tableEvent)

		// The sink output should contain the same table content (sink delegates to FormatEventLine
		// which calls formatTable → formatTableWidth with the current terminal width).
		// We verify structural properties here: truncation marker must appear at width 80.
		assert.Contains(t, got, "…")
		for i, line := range strings.Split(got, "\n") {
			w := displayWidth(line)
			if w > 80 {
				t.Errorf("line %d has display width %d (>80): %q", i, w, line)
			}
		}
	})

	t.Run("wide terminal no truncation via formatTableWidth", func(t *testing.T) {
		t.Parallel()
		got := formatTableWidth(tableEvent, 200)
		assert.NotContains(t, got, "…")
		assert.Contains(t, got, "8245db0d-5c05-4209-90f0-51ec48446a58")
	})

	t.Run("very narrow terminal still renders", func(t *testing.T) {
		t.Parallel()
		got := formatTableWidth(tableEvent, 40)
		assert.NotEmpty(t, got)
		assert.Contains(t, got, "…")
	})
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
		MessageEvent{Severity: SeveritySuccess, Text: "done"},
		MessageEvent{Severity: SeverityNote, Text: "fyi"},
		AuthEvent{URL: "https://example.com"},
		SpinnerEvent{Active: true, Text: "Loading"},
		ErrorEvent{Title: "Failed", Summary: "Something broke"},
		ContainerStatusEvent{Phase: "starting", Container: "localstack"},
		LogLineEvent{Source: "container", Line: "2024-01-01 hello"},
		InstanceInfoEvent{
			EmulatorName:  "LocalStack AWS Emulator",
			Version:       "4.14.1",
			Host:          "localhost.localstack.cloud:4566",
			ContainerName: "localstack-aws",
			Uptime:        4*time.Minute + 23*time.Second,
		},
		InstanceInfoEvent{
			EmulatorName: "LocalStack AWS Emulator",
			Host:         "127.0.0.1:4566",
		},
		TableEvent{
			Headers: []string{"SERVICE", "RESOURCE", "REGION", "ACCOUNT"},
			Rows: [][]string{
				{"Lambda", "handler", "us-east-1", "000000000000"},
				{"S3", "my-bucket", "us-east-1", "000000000000"},
			},
		},
	}

	for _, event := range events {
		var out bytes.Buffer
		sink := NewPlainSink(&out)

		switch e := event.(type) {
		case MessageEvent:
			Emit(sink, e)
		case AuthEvent:
			Emit(sink, e)
		case SpinnerEvent:
			Emit(sink, e)
		case ErrorEvent:
			Emit(sink, e)
		case ContainerStatusEvent:
			Emit(sink, e)
		case LogLineEvent:
			Emit(sink, e)
		case InstanceInfoEvent:
			Emit(sink, e)
		case TableEvent:
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
