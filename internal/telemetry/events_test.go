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

// captureEvents wires a fake analytics server behind an in-process flush.
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
	return NewWithInProcessFlush(srv.URL), ch
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
	env := c.GetEnvironment(context.Background())

	assert.Equal(t, version.Version(), env.LstkVersion)
	assert.Equal(t, "ls-abc123", env.AuthTokenID)
	assert.Equal(t, runtime.GOOS, env.OS)
	assert.Equal(t, runtime.GOARCH, env.Arch)
	assert.NotEmpty(t, env.MachineID)
}

func TestGetEnvironment_OmitsAuthTokenWhenEmpty(t *testing.T) {
	c := New("http://localhost", false)
	env := c.GetEnvironment(context.Background())
	assert.Empty(t, env.AuthTokenID)
}

func TestEmitCommand_SendsCorrectEventNameAndStructure(t *testing.T) {
	tel, ch := captureEvents(t)

	tel.SetAuthToken("ls-token")
	tel.EmitCommand(context.Background(), CommandParameters{Command: "start", Flags: []string{"--non-interactive"}}, CommandResult{DurationMS: 1200})

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

	tel.EmitCommand(context.Background(), CommandParameters{Command: "start"}, CommandResult{DurationMS: 50, ExitCode: 1, ErrorMsg: "port 4566 already in use"})

	got := drainEvent(t, tel, ch)
	payload := got["payload"].(map[string]any)
	result := payload["result"].(map[string]any)
	assert.Equal(t, "port 4566 already in use", result["error_msg"])
	assert.InDelta(t, 1, result["exit_code"], 0)
}

func TestEmitCommand_RecordsSubcommandAndRealExitCode(t *testing.T) {
	tel, ch := captureEvents(t)

	code := 252
	tel.EmitCommand(context.Background(),
		CommandParameters{Command: "aws", Subcommand: "s3 ls", Proxied: true},
		CommandResult{DurationMS: 80, ExitCode: 252, ErrorMsg: "exit status 252", ProxyExitCode: &code})

	got := drainEvent(t, tel, ch)
	payload := got["payload"].(map[string]any)
	params := payload["parameters"].(map[string]any)
	assert.Equal(t, "aws", params["command"])
	assert.Equal(t, "s3 ls", params["subcommand"])
	assert.Equal(t, true, params["proxied"])
	result := payload["result"].(map[string]any)
	assert.InDelta(t, 252, result["exit_code"], 0)
	assert.InDelta(t, 252, result["proxy_exit_code"], 0)
}

// The pipe reads the presence of proxy_exit_code as "the tool ran", so a tool
// that exited 0 must still emit the key with 0.
func TestEmitCommand_EmitsZeroProxyExitCodeWhenToolSucceeded(t *testing.T) {
	tel, ch := captureEvents(t)

	code := 0
	tel.EmitCommand(context.Background(),
		CommandParameters{Command: "aws", Subcommand: "s3 ls", Proxied: true},
		CommandResult{DurationMS: 80, ProxyExitCode: &code})

	got := drainEvent(t, tel, ch)
	payload := got["payload"].(map[string]any)
	result := payload["result"].(map[string]any)
	require.Contains(t, result, "proxy_exit_code")
	assert.InDelta(t, 0, result["proxy_exit_code"], 0)
}

// proxied and cancelled are sent as false, not omitted: JSONHas on either key
// marks a post-cutover row. A nil ProxyExitCode must be absent, not null,
// which JSONHas also reports as present.
func TestEmitCommand_ZeroValueResultShapesTheCutoverMarkers(t *testing.T) {
	tel, ch := captureEvents(t)

	tel.EmitCommand(context.Background(),
		CommandParameters{Command: "aws", Subcommand: "s3 ls", Proxied: true},
		CommandResult{DurationMS: 80, ExitCode: 1, ErrorMsg: "runtime not healthy"})

	got := drainEvent(t, tel, ch)
	payload := got["payload"].(map[string]any)
	params := payload["parameters"].(map[string]any)
	require.Contains(t, params, "proxied")
	result := payload["result"].(map[string]any)
	require.Contains(t, result, "cancelled")
	assert.Equal(t, false, result["cancelled"])
	assert.NotContains(t, result, "proxy_exit_code")
	assert.NotContains(t, result, "proxy_error", "replaced by proxied/proxy_exit_code/cancelled")
}

func TestEmitCommand_RecordsCancellation(t *testing.T) {
	tel, ch := captureEvents(t)

	tel.EmitCommand(context.Background(),
		CommandParameters{Command: "start"},
		CommandResult{DurationMS: 80, ExitCode: 1, ErrorMsg: "context canceled", Cancelled: true})

	got := drainEvent(t, tel, ch)
	payload := got["payload"].(map[string]any)
	params := payload["parameters"].(map[string]any)
	assert.Equal(t, false, params["proxied"])
	result := payload["result"].(map[string]any)
	assert.Equal(t, true, result["cancelled"])
}

func TestEmitCommand_OmitsSubcommandWhenEmpty(t *testing.T) {
	tel, ch := captureEvents(t)

	tel.EmitCommand(context.Background(), CommandParameters{Command: "start"}, CommandResult{DurationMS: 80})

	got := drainEvent(t, tel, ch)
	payload := got["payload"].(map[string]any)
	params := payload["parameters"].(map[string]any)
	_, present := params["subcommand"]
	assert.False(t, present, "empty subcommand should be omitted from the payload")
}

func TestEmitCommand_IsNoOpWhenDisabled(t *testing.T) {
	received := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- struct{}{}
	}))
	defer srv.Close()

	tel := New(srv.URL, true) // disabled
	tel.EmitCommand(context.Background(), CommandParameters{Command: "start"}, CommandResult{})
	tel.Close()

	select {
	case <-received:
		t.Fatal("disabled client should not send events")
	default:
	}
}

// error_code and error_category are the "why" axis: present only when the
// failing site classified its error, absent otherwise, so the pipe can
// measure classification coverage.
func TestEmitCommand_RecordsErrorCodeAndCategoryWhenClassified(t *testing.T) {
	tel, ch := captureEvents(t)

	tel.EmitCommand(context.Background(),
		CommandParameters{Command: "aws", Proxied: true},
		CommandResult{ExitCode: 1, ErrorMsg: "--account must be a 12-digit AWS account id", ErrorCode: "VALIDATION_ERROR", ErrorCategory: "USAGE"})

	got := drainEvent(t, tel, ch)
	result := got["payload"].(map[string]any)["result"].(map[string]any)
	assert.Equal(t, "VALIDATION_ERROR", result["error_code"])
	assert.Equal(t, "USAGE", result["error_category"])
}

func TestEmitCommand_OmitsErrorCodeWhenUnclassified(t *testing.T) {
	tel, ch := captureEvents(t)

	tel.EmitCommand(context.Background(), CommandParameters{Command: "start"}, CommandResult{ExitCode: 1, ErrorMsg: "boom"})

	got := drainEvent(t, tel, ch)
	result := got["payload"].(map[string]any)["result"].(map[string]any)
	assert.NotContains(t, result, "error_code")
	assert.NotContains(t, result, "error_category")
}
