package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONSink_EmitsNDJSON(t *testing.T) {
	var out bytes.Buffer
	sink := NewJSONSink(&out)

	Emit(sink, MessageEvent{Severity: SeverityInfo, Text: "hello"})
	Emit(sink, MessageEvent{Severity: SeveritySuccess, Text: "done"})

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	require.Len(t, lines, 2)

	for _, line := range lines {
		var env map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &env))
		assert.Equal(t, "message", env["type"])
	}
}

func TestJSONSink_SuppressesSpinnerStop(t *testing.T) {
	var out bytes.Buffer
	sink := NewJSONSink(&out)

	Emit(sink, SpinnerEvent{Active: false})

	assert.Empty(t, out.String())
}

func TestJSONSink_ErrCapturesWriteError(t *testing.T) {
	writeErr := assert.AnError
	sink := NewJSONSink(&failingWriter{err: writeErr})

	Emit(sink, MessageEvent{Severity: SeverityInfo, Text: "hello"})

	assert.Equal(t, writeErr, sink.Err())
}

func TestJSONSink_ErrReturnsNilOnSuccess(t *testing.T) {
	var out bytes.Buffer
	sink := NewJSONSink(&out)

	Emit(sink, MessageEvent{Severity: SeverityInfo, Text: "hello"})

	assert.NoError(t, sink.Err())
}
