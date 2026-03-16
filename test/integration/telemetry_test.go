package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockAnalyticsServer returns a test server that records received analytics events.
// The returned channel receives one value per event (unwrapped from the events array).
func mockAnalyticsServer(t *testing.T) (*httptest.Server, <-chan map[string]any) {
	t.Helper()
	ch := make(chan map[string]any, 10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var req struct {
			Events []map[string]any `json:"events"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		for _, event := range req.Events {
			ch <- event
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv, ch
}

func TestStartCommandSendsTelemetryEvent(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Pre-start a container so lstk start exits immediately after telemetry fires,
	// without needing a real token or license server.
	startTestContainer(t, ctx)

	analyticsSrv, events := mockAnalyticsServer(t)

	cmd := exec.CommandContext(ctx, binaryPath(), "start")
	cmd.Env = env.With(env.AuthToken, "fake-token").
		With(env.AnalyticsEndpoint, analyticsSrv.URL)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "lstk start failed: %s", out)
	requireExitCode(t, 0, err)

	// The telemetry goroutine is async; wait up to 3s for the event to arrive.
	select {
	case event := <-events:
		assert.Equal(t, "cli_cmd", event["name"])

		metadata, ok := event["metadata"].(map[string]any)
		require.True(t, ok)
		_, err := uuid.Parse(metadata["session_id"].(string))
		assert.NoError(t, err, "session_id should be a valid UUID")
		_, err = time.Parse("2006-01-02 15:04:05.000000", metadata["client_time"].(string))
		assert.NoError(t, err, "client_time should match expected format")

		payload, ok := event["payload"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "lstk start", payload["cmd"])
		assert.NotEmpty(t, payload["version"])
		assert.Equal(t, runtime.GOOS, payload["os"])
		assert.Equal(t, runtime.GOARCH, payload["arch"])
		assert.NotEmpty(t, payload["machine_id"], "machine_id should be present")
		assert.Equal(t, os.Getenv("CI") != "", payload["is_ci"])
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for telemetry event")
	}
}

func TestStartCommandSucceedsWhenAnalyticsEndpointUnreachable(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	startTestContainer(t, ctx)

	cmd := exec.CommandContext(ctx, binaryPath(), "start")
	cmd.Env = env.With(env.AuthToken, "fake-token").
		With(env.AnalyticsEndpoint, "http://127.0.0.1:1")
	out, err := cmd.CombinedOutput()

	require.NoError(t, err, "lstk start should succeed even when analytics endpoint is unreachable: %s", out)
	requireExitCode(t, 0, err)
}

func TestStartCommandDoesNotSendTelemetryWhenDisabled(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	startTestContainer(t, ctx)

	analyticsSrv, events := mockAnalyticsServer(t)

	cmd := exec.CommandContext(ctx, binaryPath(), "start")
	cmd.Env = env.With(env.AuthToken, "fake-token").
		With(env.AnalyticsEndpoint, analyticsSrv.URL).
		With(env.DisableEvents, "1")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "lstk start failed: %s", out)
	requireExitCode(t, 0, err)

	// Wait long enough that a goroutine would have fired if enabled.
	select {
	case event := <-events:
		t.Fatalf("unexpected telemetry event received: %v", event)
	case <-time.After(time.Second):
		// No event received — correct.
	}
}
