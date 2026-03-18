package telemetry

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"github.com/localstack/lstk/internal/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func captureEvents(t *testing.T) (*Client, <-chan map[string]any) {
	t.Helper()
	ch := make(chan map[string]any, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		var req struct {
			Events []map[string]any `json:"events"`
		}
		if assert.NoError(t, json.Unmarshal(body, &req)) && len(req.Events) > 0 {
			ch <- req.Events[0]
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL, false), ch
}

func drainEvent(t *testing.T, tel *Client, ch <-chan map[string]any) map[string]any {
	t.Helper()
	tel.Close()
	select {
	case ev := <-ch:
		return ev
	default:
		t.Fatal("no telemetry event received")
		return nil
	}
}

func TestGetEnvironment_PopulatesAllFields(t *testing.T) {
	c := New("http://localhost", false)
	c.SetAuthToken("ls-abc123")
	env := c.GetEnvironment()

	assert.Equal(t, version.Version(), env.LstkVersion)
	assert.Equal(t, "ls-abc123", env.AuthTokenID)
	assert.Equal(t, runtime.GOOS, env.OS)
	assert.Equal(t, runtime.GOARCH, env.Arch)
	assert.NotEmpty(t, env.MachineID)
}

func TestGetEnvironment_OmitsAuthTokenWhenEmpty(t *testing.T) {
	c := New("http://localhost", false)
	env := c.GetEnvironment()
	assert.Empty(t, env.AuthTokenID)
}

func TestEmitCommand_SendsCorrectEventNameAndStructure(t *testing.T) {
	tel, ch := captureEvents(t)

	tel.SetAuthToken("ls-token")
	tel.Emit(context.Background(), "lstk_command", ToMap(CommandEvent{
		Environment: tel.GetEnvironment(),
		Parameters:  CommandParameters{Command: "start", Flags: []string{"--non-interactive"}},
		Result:      CommandResult{DurationMS: 1200, ExitCode: 0},
	}))

	got := drainEvent(t, tel, ch)

	assert.Equal(t, "lstk_command", got["name"])

	metadata, ok := got["metadata"].(map[string]any)
	require.True(t, ok)
	assert.NotEmpty(t, metadata["session_id"])
	_, err := time.Parse("2006-01-02 15:04:05.000000", metadata["client_time"].(string))
	assert.NoError(t, err)

	payload, ok := got["payload"].(map[string]any)
	require.True(t, ok)

	env, ok := payload["environment"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, version.Version(), env["lstk_version"])
	assert.Equal(t, "ls-token", env["auth_token_id"])

	params, ok := payload["parameters"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "start", params["command"])
	assert.Equal(t, []any{"--non-interactive"}, params["flags"])

	result, ok := payload["result"].(map[string]any)
	require.True(t, ok)
	assert.InDelta(t, 1200, result["duration_ms"], 1)
	assert.InDelta(t, 0, result["exit_code"], 0)
}

func TestEmitCommand_IncludesErrorMsgOnFailure(t *testing.T) {
	tel, ch := captureEvents(t)

	tel.Emit(context.Background(), "lstk_command", ToMap(CommandEvent{
		Environment: tel.GetEnvironment(),
		Parameters:  CommandParameters{Command: "start"},
		Result:      CommandResult{DurationMS: 50, ExitCode: 1, ErrorMsg: "port 4566 already in use"},
	}))

	got := drainEvent(t, tel, ch)
	payload := got["payload"].(map[string]any)
	result := payload["result"].(map[string]any)
	assert.Equal(t, "port 4566 already in use", result["error_msg"])
	assert.InDelta(t, 1, result["exit_code"], 0)
}

func TestEmitCommand_IsNoOpWhenDisabled(t *testing.T) {
	received := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- struct{}{}
	}))
	defer srv.Close()

	tel := New(srv.URL, true) // disabled
	tel.Emit(context.Background(), "lstk_command", ToMap(CommandEvent{}))
	tel.Close()

	select {
	case <-received:
		t.Fatal("disabled client should not send events")
	default:
	}
}
