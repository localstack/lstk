package output

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatEventJSON_MessageEvent(t *testing.T) {
	tests := []struct {
		name     string
		event    MessageEvent
		severity string
	}{
		{"info", MessageEvent{Severity: SeverityInfo, Text: "hello"}, "info"},
		{"success", MessageEvent{Severity: SeveritySuccess, Text: "done"}, "success"},
		{"note", MessageEvent{Severity: SeverityNote, Text: "fyi"}, "note"},
		{"warning", MessageEvent{Severity: SeverityWarning, Text: "careful"}, "warning"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, ok := FormatEventJSON(tt.event)
			require.True(t, ok)

			var env map[string]any
			require.NoError(t, json.Unmarshal(b, &env))
			assert.Equal(t, "message", env["type"])

			data := env["data"].(map[string]any)
			assert.Equal(t, tt.severity, data["severity"])
			assert.Equal(t, tt.event.Text, data["text"])
		})
	}
}

func TestFormatEventJSON_ErrorEvent(t *testing.T) {
	b, ok := FormatEventJSON(ErrorEvent{
		Title:   "Connection failed",
		Summary: "Cannot connect",
		Actions: []ErrorAction{{Label: "Start Docker:", Value: "open -a Docker"}},
	})
	require.True(t, ok)

	var env map[string]any
	require.NoError(t, json.Unmarshal(b, &env))
	assert.Equal(t, "error", env["type"])

	data := env["data"].(map[string]any)
	assert.Equal(t, "Connection failed", data["title"])
	assert.Equal(t, "Cannot connect", data["summary"])
}

func TestFormatEventJSON_ContainerStatusEvent(t *testing.T) {
	b, ok := FormatEventJSON(ContainerStatusEvent{Phase: "ready", Container: "localstack", Detail: "abc123"})
	require.True(t, ok)

	var env map[string]any
	require.NoError(t, json.Unmarshal(b, &env))
	assert.Equal(t, "status", env["type"])

	data := env["data"].(map[string]any)
	assert.Equal(t, "ready", data["phase"])
	assert.Equal(t, "localstack", data["container"])
	assert.Equal(t, "abc123", data["detail"])
}

func TestFormatEventJSON_ProgressEvent(t *testing.T) {
	t.Run("with total", func(t *testing.T) {
		b, ok := FormatEventJSON(ProgressEvent{
			Container: "localstack", LayerID: "abc", Status: "Downloading", Current: 50, Total: 100,
		})
		require.True(t, ok)

		var env map[string]any
		require.NoError(t, json.Unmarshal(b, &env))
		assert.Equal(t, "progress", env["type"])

		data := env["data"].(map[string]any)
		assert.Equal(t, float64(50), data["percent"])
	})

	t.Run("empty suppressed", func(t *testing.T) {
		_, ok := FormatEventJSON(ProgressEvent{Container: "localstack", LayerID: "abc"})
		assert.False(t, ok)
	})
}

func TestFormatEventJSON_SpinnerSuppressed(t *testing.T) {
	_, ok := FormatEventJSON(SpinnerEvent{Active: true, Text: "Loading"})
	assert.False(t, ok)
}

func TestFormatEventJSON_AuthEvent(t *testing.T) {
	b, ok := FormatEventJSON(AuthEvent{Code: "ABC123", URL: "https://example.com"})
	require.True(t, ok)

	var env map[string]any
	require.NoError(t, json.Unmarshal(b, &env))
	assert.Equal(t, "auth", env["type"])

	data := env["data"].(map[string]any)
	assert.Equal(t, "ABC123", data["code"])
	assert.Equal(t, "https://example.com", data["url"])
}

func TestFormatEventJSON_ContainerLogLineEvent(t *testing.T) {
	b, ok := FormatEventJSON(ContainerLogLineEvent{Line: "hello from container"})
	require.True(t, ok)

	var env map[string]any
	require.NoError(t, json.Unmarshal(b, &env))
	assert.Equal(t, "log", env["type"])

	data := env["data"].(map[string]any)
	assert.Equal(t, "hello from container", data["line"])
}

func TestFormatEventJSON_UserInputRequestSuppressed(t *testing.T) {
	_, ok := FormatEventJSON(UserInputRequestEvent{Prompt: "test"})
	assert.False(t, ok)
}
