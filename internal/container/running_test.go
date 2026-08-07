package container

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func nopSink() output.Sink {
	return output.SinkFunc(func(output.Event) {})
}

// countingInfoServer serves /_localstack/info and counts requests, so tests can
// assert the HTTP probe did or did not run.
func countingInfoServer(t *testing.T) (host string, calls *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_localstack/info" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		n.Add(1)
		_, _ = w.Write([]byte(`{"version":"4.16.0","edition":"community"}`))
	}))
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "http://"), &n
}

func awsTestContainer() config.ContainerConfig {
	return config.ContainerConfig{Type: config.EmulatorAWS, Port: config.DefaultPort}
}

func TestResolveEmulatorManagedContainerSkipsProbe(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	c := awsTestContainer()
	mockRT.EXPECT().IsRunning(gomock.Any(), c.Name()).Return(true, nil)

	host, calls := countingInfoServer(t)

	resolved, err := ResolveEmulator(context.Background(), mockRT, c, host)
	require.NoError(t, err)
	assert.Equal(t, c.Name(), resolved.ContainerName)
	assert.False(t, resolved.External)
	assert.True(t, resolved.Found())
	assert.Equal(t, int32(0), calls.Load(), "probe must not run when a managed container is found")
}

func TestResolveEmulatorFallsBackToProbe(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	c := awsTestContainer()
	containerPort, err := c.ContainerPort()
	require.NoError(t, err)

	mockRT.EXPECT().IsRunning(gomock.Any(), c.Name()).Return(false, nil)
	mockRT.EXPECT().FindRunningByImage(gomock.Any(), config.KnownImageReposForType(c.Type), containerPort).Return(nil, nil)
	// Wrong-type guard: no known LocalStack container of any type is running.
	mockRT.EXPECT().FindRunningByImage(gomock.Any(), config.KnownImageRepos(), containerPort).Return(nil, nil)

	host, calls := countingInfoServer(t)

	resolved, err := ResolveEmulator(context.Background(), mockRT, c, host)
	require.NoError(t, err)
	assert.Empty(t, resolved.ContainerName)
	assert.True(t, resolved.External)
	assert.True(t, resolved.Found())
	require.NotNil(t, resolved.Info)
	assert.Equal(t, "4.16.0", resolved.Info.Version)
	assert.Equal(t, int32(1), calls.Load())
}

func TestResolveEmulatorNilRuntimeProbesDirectly(t *testing.T) {
	host, _ := countingInfoServer(t)

	resolved, err := ResolveEmulator(context.Background(), nil, awsTestContainer(), host)
	require.NoError(t, err)
	assert.True(t, resolved.External)
	assert.True(t, resolved.Found())
}

func TestResolveEmulatorNilRuntimeNothingListening(t *testing.T) {
	resolved, err := ResolveEmulator(context.Background(), nil, awsTestContainer(), "127.0.0.1:1")
	require.NoError(t, err)
	assert.False(t, resolved.Found())
}

func TestResolveEmulatorProbeAnswerFromOtherEmulatorContainer(t *testing.T) {
	// A known LocalStack container of a different type is running: the probe
	// answer is that container, not an external instance, so resolution must
	// report not-found and let callers keep today's type-mismatch errors.
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	c := awsTestContainer()
	containerPort, err := c.ContainerPort()
	require.NoError(t, err)

	mockRT.EXPECT().IsRunning(gomock.Any(), c.Name()).Return(false, nil)
	mockRT.EXPECT().FindRunningByImage(gomock.Any(), config.KnownImageReposForType(c.Type), containerPort).Return(nil, nil)
	mockRT.EXPECT().FindRunningByImage(gomock.Any(), config.KnownImageRepos(), containerPort).
		Return(&runtime.RunningContainer{Name: "localstack-snowflake", Image: "localstack/snowflake:latest"}, nil)

	host, _ := countingInfoServer(t)

	resolved, err := ResolveEmulator(context.Background(), mockRT, c, host)
	require.NoError(t, err)
	assert.False(t, resolved.Found())
}

func TestFirstReachableEmulatorManagedContainer(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	c := awsTestContainer()
	mockRT.EXPECT().IsHealthy(gomock.Any()).Return(nil)
	mockRT.EXPECT().IsRunning(gomock.Any(), c.Name()).Return(true, nil)

	target, resolved, err := FirstReachableEmulator(context.Background(), mockRT, nopSink(), []config.ContainerConfig{c}, "127.0.0.1:1")
	require.NoError(t, err)
	assert.Equal(t, c.Type, target.Type)
	assert.True(t, resolved.Found())
}

func TestFirstReachableEmulatorDockerDownProbeOK(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().IsHealthy(gomock.Any()).Return(assert.AnError)

	host, _ := countingInfoServer(t)

	target, resolved, err := FirstReachableEmulator(context.Background(), mockRT, nopSink(), []config.ContainerConfig{awsTestContainer()}, host)
	require.NoError(t, err)
	assert.Equal(t, config.EmulatorAWS, target.Type)
	assert.True(t, resolved.External)
}

func TestFirstReachableEmulatorDockerDownNothingListening(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().IsHealthy(gomock.Any()).Return(assert.AnError)
	mockRT.EXPECT().EmitUnhealthyError(gomock.Any(), assert.AnError)

	_, _, err := FirstReachableEmulator(context.Background(), mockRT, nopSink(), []config.ContainerConfig{awsTestContainer()}, "127.0.0.1:1")
	require.Error(t, err)
	assert.True(t, output.IsSilent(err))
}

func TestFirstReachableEmulatorDockerHealthyNothingFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	c := awsTestContainer()
	containerPort, err := c.ContainerPort()
	require.NoError(t, err)
	mockRT.EXPECT().IsHealthy(gomock.Any()).Return(nil)
	mockRT.EXPECT().IsRunning(gomock.Any(), c.Name()).Return(false, nil)
	mockRT.EXPECT().FindRunningByImage(gomock.Any(), config.KnownImageReposForType(c.Type), containerPort).Return(nil, nil)

	_, resolved, err := FirstReachableEmulator(context.Background(), mockRT, nopSink(), []config.ContainerConfig{c}, "127.0.0.1:1")
	require.NoError(t, err)
	assert.False(t, resolved.Found())
}

func TestResolveEmulatorRuntimeErrorPropagates(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	c := awsTestContainer()
	mockRT.EXPECT().IsRunning(gomock.Any(), c.Name()).Return(false, assert.AnError)

	_, err := ResolveEmulator(context.Background(), mockRT, c, "127.0.0.1:1")
	assert.Error(t, err)
}
