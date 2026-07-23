package container

import (
	"context"
	"errors"
	"net"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/localstack/lstk/internal/runtime"
)

// busyPort binds a listener for the duration of the test and returns its port.
func busyPort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })
	return strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
}

// freePort returns a port that was just released and is very likely free.
func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
	require.NoError(t, l.Close())
	return port
}

func TestDropBusyOptionalPortsDropsBusyAndWarns(t *testing.T) {
	busy := busyPort(t)
	free := freePort(t)

	sink := &recordingSink{}
	kept := dropBusyOptionalPorts(sink, runtime.FlavorDockerDesktop, "4566", []runtime.PortMapping{
		{ContainerPort: "443", HostPort: busy, Optional: true},
		{ContainerPort: "8443", HostPort: free, Optional: true},
	})

	require.Len(t, kept, 1)
	assert.Equal(t, free, kept[0].HostPort)

	texts := sink.messageTexts()
	require.Len(t, texts, 1)
	assert.Contains(t, texts[0], "Port "+busy+" is in use — starting without it")
	assert.Contains(t, texts[0], "https://localhost:4566")
}

func TestDropBusyOptionalPortsPassesRequiredThrough(t *testing.T) {
	busy := busyPort(t)

	sink := &recordingSink{}
	kept := dropBusyOptionalPorts(sink, runtime.FlavorDockerDesktop, "4566", []runtime.PortMapping{
		{ContainerPort: "443", HostPort: busy, Optional: false},
	})

	require.Len(t, kept, 1, "required mappings are the caller's responsibility and must never be dropped")
	assert.Empty(t, sink.messageTexts())
}

func TestOptionalPortDropWarningRancherHint(t *testing.T) {
	busyHint := optionalPortDropWarning(runtime.FlavorRancherDesktop, "443", "4566", portBusy)
	assert.Contains(t, busyHint, "rdctl set --kubernetes.options.traefik=false")

	deniedHint := optionalPortDropWarning(runtime.FlavorRancherDesktop, "443", "4566", portBindDenied)
	assert.Contains(t, deniedHint, "permission denied")
	assert.Contains(t, deniedHint, "Administrative Access")

	podmanDenied := optionalPortDropWarning(runtime.FlavorPodman, "443", "4566", portBindDenied)
	assert.Contains(t, podmanDenied, "podman machine set --rootful")

	withoutHint := optionalPortDropWarning(runtime.FlavorDockerDesktop, "443", "4566", portBusy)
	assert.NotContains(t, withoutHint, "rdctl")
}

func TestFailedOptionalPortBindMatchesDaemonError(t *testing.T) {
	mappings := []runtime.PortMapping{
		{ContainerPort: "443", HostPort: "443", Optional: true},
		{ContainerPort: "8443", HostPort: "8443", Optional: false},
	}

	daemonErr := errors.New(`Error response from daemon: something went wrong with the request: "listen tcp 127.0.0.1:443: bind: permission denied\n"`)
	assert.Equal(t, 0, failedOptionalPortBind(daemonErr, mappings))

	inUseErr := errors.New(`Error response from daemon: driver failed programming external connectivity: listen tcp 127.0.0.1:443: bind: address already in use`)
	assert.Equal(t, 0, failedOptionalPortBind(inUseErr, mappings))

	requiredErr := errors.New(`listen tcp 127.0.0.1:8443: bind: permission denied`)
	assert.Equal(t, -1, failedOptionalPortBind(requiredErr, mappings), "required ports must not be silently dropped")

	assert.Equal(t, -1, failedOptionalPortBind(errors.New("image not found"), mappings))
	assert.Equal(t, -1, failedOptionalPortBind(nil, mappings))
}

func TestStartWithOptionalPortFallbackRetriesWithoutDeniedPort(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)

	c := runtime.ContainerConfig{
		Name: "localstack-aws",
		Port: "4566",
		ExtraPorts: []runtime.PortMapping{
			{ContainerPort: "443", HostPort: "443", Optional: true},
		},
	}

	bindErr := errors.New(`Error response from daemon: something went wrong with the request: "listen tcp 127.0.0.1:443: bind: permission denied\n"`)
	exitCh := make(chan runtime.ExitResult, 1)

	mockRT.EXPECT().Start(gomock.Any(), c).Return("", nil, bindErr)
	mockRT.EXPECT().Flavor().Return(runtime.FlavorRancherDesktop)
	mockRT.EXPECT().Remove(gomock.Any(), c.Name).Return(nil)
	retried := c
	retried.ExtraPorts = []runtime.PortMapping{}
	mockRT.EXPECT().Start(gomock.Any(), retried).Return("id-1", exitCh, nil)

	sink := &recordingSink{}
	id, _, err := startWithOptionalPortFallback(context.Background(), mockRT, sink, c)
	require.NoError(t, err)
	assert.Equal(t, "id-1", id)

	texts := sink.messageTexts()
	require.Len(t, texts, 1)
	assert.Contains(t, texts[0], "Port 443 cannot be published (bind: permission denied) — starting without it")
	assert.Contains(t, texts[0], "Administrative Access")
}

func TestStartWithOptionalPortFallbackPassesThroughOtherErrors(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)

	c := runtime.ContainerConfig{Name: "localstack-aws", Port: "4566"}
	startErr := errors.New("no space left on device")
	mockRT.EXPECT().Start(gomock.Any(), c).Return("", nil, startErr)

	sink := &recordingSink{}
	_, _, err := startWithOptionalPortFallback(context.Background(), mockRT, sink, c)
	assert.ErrorIs(t, err, startErr)
	assert.Empty(t, sink.messageTexts())
}

func TestPortConflictActions(t *testing.T) {
	actions := portConflictActions(runtime.FlavorRancherDesktop, "443")
	require.Len(t, actions, 1)
	assert.Equal(t, "rdctl set --kubernetes.options.traefik=false", actions[0].Value)

	assert.Empty(t, portConflictActions(runtime.FlavorDockerDesktop, "443"))
	assert.Empty(t, portConflictActions(runtime.FlavorRancherDesktop, "8443"))
}
