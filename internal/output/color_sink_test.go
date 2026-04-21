package output

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestColorSink_NoColor_FallsBackToPlain(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	var out bytes.Buffer
	sink := NewColorSink(&out)
	assert.False(t, sink.colored)

	Emit(sink, ErrorEvent{
		Title:   "Connection failed",
		Summary: "Cannot connect to Docker",
		Actions: []ErrorAction{{Label: "Start Docker:", Value: "open -a Docker"}},
	})

	expected := "Error: Connection failed\n  Cannot connect to Docker\n  ==> Start Docker: open -a Docker\n"
	assert.Equal(t, expected, out.String())
}

func TestColorSink_NonTTY_FallsBackToPlain(t *testing.T) {
	// bytes.Buffer is not an *os.File, so isColorTerminal returns false.
	var out bytes.Buffer
	sink := NewColorSink(&out)
	assert.False(t, sink.colored)

	Emit(sink, ErrorEvent{Title: "Something broke"})
	assert.Equal(t, "Error: Something broke\n", out.String())
}

func TestColorSink_ColoredMode_RendersErrorWithMarkers(t *testing.T) {
	// Force colored mode directly to test rendering without a real TTY.
	var out bytes.Buffer
	sink := &ColorSink{out: &out, colored: true}

	Emit(sink, ErrorEvent{
		Title:   "Docker is not available",
		Summary: "cannot connect to daemon",
		Actions: []ErrorAction{
			{Label: "Start Docker:", Value: "open -a Docker"},
			{Label: "Install Docker:", Value: "https://docs.docker.com/get-docker/"},
		},
	})

	got := out.String()
	assert.Contains(t, got, "Docker is not available")
	assert.Contains(t, got, "cannot connect to daemon")
	assert.Contains(t, got, "Start Docker:")
	assert.Contains(t, got, "open -a Docker")
	assert.Contains(t, got, "Install Docker:")
}

func TestColorSink_NonErrorEvents_DelegateToPlain(t *testing.T) {
	var out bytes.Buffer
	sink := &ColorSink{out: &out, colored: true}

	EmitInfo(sink, "hello world")
	assert.Equal(t, "hello world\n", out.String())
}

func TestIsColorTerminal_NoColorEnv(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var out bytes.Buffer
	assert.False(t, isColorTerminal(&out))
}
