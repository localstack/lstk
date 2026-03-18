package container

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/telemetry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// newCapturingTelClient starts an httptest server that captures emitted events.
func newCapturingTelClient(t *testing.T) (*telemetry.Client, <-chan map[string]any) {
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
	return telemetry.New(srv.URL, false), ch
}

func TestStop_EmitsLifecycleStopEvent(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().IsRunning(gomock.Any(), "localstack-aws").Return(true, nil)
	mockRT.EXPECT().Stop(gomock.Any(), "localstack-aws").Return(nil)

	tel, ch := newCapturingTelClient(t)

	containers := []config.ContainerConfig{{Type: config.EmulatorAWS, Port: "4566"}}
	sink := output.NewPlainSink(io.Discard)

	err := Stop(context.Background(), mockRT, sink, containers, StopOptions{
		Telemetry: tel,
		AuthToken: "ls-abc",
	})
	require.NoError(t, err)
	tel.Close()

	var got map[string]any
	select {
	case got = <-ch:
	default:
		t.Fatal("no lifecycle event received")
	}

	assert.Equal(t, "lstk_lifecycle", got["name"])
	payload := got["payload"].(map[string]any)
	assert.Equal(t, telemetry.LifecycleStop, payload["event_type"])
	assert.Equal(t, "aws", payload["emulator"])

	env := payload["environment"].(map[string]any)
	assert.Equal(t, "ls-abc", env["auth_token_id"])
}

func TestStop_SkipsTelemetryWhenNil(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().IsRunning(gomock.Any(), "localstack-aws").Return(true, nil)
	mockRT.EXPECT().Stop(gomock.Any(), "localstack-aws").Return(nil)

	containers := []config.ContainerConfig{{Type: config.EmulatorAWS, Port: "4566"}}
	sink := output.NewPlainSink(io.Discard)

	err := Stop(context.Background(), mockRT, sink, containers, StopOptions{})
	require.NoError(t, err)
}

func TestTelCtx_EmitStartError_IsNoOpWhenTelNil(t *testing.T) {
	tc := telCtx{tel: nil}
	c := runtime.ContainerConfig{EmulatorType: "aws", Image: "localstack/localstack-pro:latest"}
	// Must not panic.
	tc.emitEmulatorStartError(context.Background(), c, telemetry.ErrCodePortConflict, "port 4566 in use")
}

func TestTelCtx_EmitStartError_SendsLifecycleEvent(t *testing.T) {
	tel, ch := newCapturingTelClient(t)
	tc := telCtx{tel: tel, authToken: "ls-xyz"}

	c := runtime.ContainerConfig{
		EmulatorType: "aws",
		Image:        "localstack/localstack-pro:latest",
	}
	tc.emitEmulatorStartError(context.Background(), c, telemetry.ErrCodePortConflict, "port 4566 already in use")
	tel.Close()

	var got map[string]any
	select {
	case got = <-ch:
	default:
		t.Fatal("no event received")
	}

	assert.Equal(t, "lstk_lifecycle", got["name"])
	payload := got["payload"].(map[string]any)
	assert.Equal(t, telemetry.LifecycleStartError, payload["event_type"])
	assert.Equal(t, "aws", payload["emulator"])
	assert.Equal(t, telemetry.ErrCodePortConflict, payload["error_code"])
	assert.Equal(t, "port 4566 already in use", payload["error_msg"])
}
