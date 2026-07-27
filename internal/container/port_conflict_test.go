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

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/telemetry"
)

// errorEvents returns the ErrorEvents captured by the sink.
func (s *recordingSink) errorEvents() []output.ErrorEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	var errs []output.ErrorEvent
	for _, e := range s.events {
		if ev, ok := e.(output.ErrorEvent); ok {
			errs = append(errs, ev)
		}
	}
	return errs
}

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
	kept := dropBusyOptionalPorts(sink, runtime.FlavorDockerDesktop, runtime.FlavorUnknown, "4566", []runtime.PortMapping{
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
	kept := dropBusyOptionalPorts(sink, runtime.FlavorDockerDesktop, runtime.FlavorUnknown, "4566", []runtime.PortMapping{
		{ContainerPort: "443", HostPort: busy, Optional: false},
	})

	require.Len(t, kept, 1, "required mappings are the caller's responsibility and must never be dropped")
	assert.Empty(t, sink.messageTexts())
}

func TestOptionalPortDropWarningRancherHint(t *testing.T) {
	busyHint := optionalPortDropWarning(runtime.FlavorRancherDesktop, runtime.FlavorUnknown, "443", "4566", portBusy)
	assert.Contains(t, busyHint, "rdctl set --kubernetes.options.traefik=false")

	deniedHint := optionalPortDropWarning(runtime.FlavorRancherDesktop, runtime.FlavorUnknown, "443", "4566", portBindDenied)
	assert.Contains(t, deniedHint, "permission denied")
	assert.Contains(t, deniedHint, "Administrative Access")

	podmanDenied := optionalPortDropWarning(runtime.FlavorPodman, runtime.FlavorUnknown, "443", "4566", portBindDenied)
	assert.Contains(t, podmanDenied, "podman machine set --rootful")

	withoutHint := optionalPortDropWarning(runtime.FlavorDockerDesktop, runtime.FlavorUnknown, "443", "4566", portBusy)
	assert.NotContains(t, withoutHint, "rdctl")
}

// On Windows the daemon host is a named pipe, so the active socket flavor is
// always unknown — installed-runtime evidence must carry the hints there.
func TestTailoredPortDropHintUsesInstalledEvidence(t *testing.T) {
	// Busy 443: Rancher merely being installed names Traefik, even when lstk is
	// connected to a different (or unclassifiable) runtime.
	assert.Contains(t,
		tailoredPortDropHint(runtime.FlavorUnknown, runtime.FlavorRancherDesktop, "443", portBusy),
		"rdctl set --kubernetes.options.traefik=false")
	assert.Contains(t,
		tailoredPortDropHint(runtime.FlavorDockerDesktop, runtime.FlavorRancherDesktop, "443", portBusy),
		"rdctl set --kubernetes.options.traefik=false")

	// Bind denied: evidence substitutes only when the active daemon is
	// unclassified — a denial under a positively identified runtime is about
	// that runtime's privileges, not some other installed one.
	assert.Contains(t,
		tailoredPortDropHint(runtime.FlavorUnknown, runtime.FlavorRancherDesktop, "443", portBindDenied),
		"Administrative Access")
	assert.Contains(t,
		tailoredPortDropHint(runtime.FlavorUnknown, runtime.FlavorPodman, "443", portBindDenied),
		"podman machine set --rootful")
	assert.Empty(t,
		tailoredPortDropHint(runtime.FlavorDockerDesktop, runtime.FlavorRancherDesktop, "443", portBindDenied))
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

func TestStartWithOptionalPortFallbackNamesLeftoverContainerWhenRemoveFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)

	c := runtime.ContainerConfig{
		Name: "localstack-aws",
		Port: "4566",
		ExtraPorts: []runtime.PortMapping{
			{ContainerPort: "443", HostPort: "443", Optional: true},
		},
	}

	bindErr := errors.New(`listen tcp 127.0.0.1:443: bind: permission denied`)
	mockRT.EXPECT().Start(gomock.Any(), c).Return("", nil, bindErr)
	mockRT.EXPECT().Flavor().Return(runtime.FlavorUnknown)
	mockRT.EXPECT().Remove(gomock.Any(), c.Name).Return(errors.New("daemon busy"))

	_, _, err := startWithOptionalPortFallback(context.Background(), mockRT, &recordingSink{}, c)
	require.Error(t, err)
	assert.ErrorIs(t, err, bindErr, "the original bind error must stay the cause")
	assert.Contains(t, err.Error(), `"localstack-aws"`, "the leftover container must be named so the user can remove it")
	assert.Contains(t, err.Error(), "docker rm -f localstack-aws")
}

func TestHealLeftoverContainerRemovesManagedCreatedLeftover(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)

	c := runtime.ContainerConfig{Name: "localstack-aws", EmulatorType: config.EmulatorAWS}
	brief := runtime.ContainerBrief{Exists: true, Created: true, Managed: true, Image: "localstack/localstack-pro:latest"}
	mockRT.EXPECT().Remove(gomock.Any(), c.Name).Return(nil)

	sink := &recordingSink{}
	err := healLeftoverContainer(context.Background(), mockRT, sink, telemetry.New("", true), c, brief)
	require.NoError(t, err)

	texts := sink.messageTexts()
	require.Len(t, texts, 1)
	assert.Contains(t, texts[0], `Removed leftover container "localstack-aws"`)
}

func TestHealLeftoverContainerRefusesForeignContainer(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)

	c := runtime.ContainerConfig{Name: "localstack-aws", EmulatorType: config.EmulatorAWS}
	// No Remove expectation: a container without the lstk label must never be removed.
	brief := runtime.ContainerBrief{Exists: true, Created: true, Managed: false, Image: "nginx:latest"}

	sink := &recordingSink{}
	err := healLeftoverContainer(context.Background(), mockRT, sink, telemetry.New("", true), c, brief)
	require.Error(t, err)
	assert.True(t, output.IsSilent(err))

	errs := sink.errorEvents()
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0].Summary, "nginx:latest")
	assert.Contains(t, errs[0].Summary, "not created by lstk")
}

func TestHealLeftoverContainerReportsFailedRemoval(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)

	c := runtime.ContainerConfig{Name: "localstack-aws", EmulatorType: config.EmulatorAWS}
	brief := runtime.ContainerBrief{Exists: true, Created: true, Managed: true, Image: "localstack/localstack-pro:latest"}
	mockRT.EXPECT().Remove(gomock.Any(), c.Name).Return(errors.New("daemon busy"))

	sink := &recordingSink{}
	err := healLeftoverContainer(context.Background(), mockRT, sink, telemetry.New("", true), c, brief)
	require.Error(t, err)

	errs := sink.errorEvents()
	require.Len(t, errs, 1)
	require.Len(t, errs[0].Actions, 1)
	assert.Equal(t, "docker rm -f localstack-aws", errs[0].Actions[0].Value)
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
	actions := portConflictActions(runtime.FlavorRancherDesktop, runtime.FlavorUnknown, "443")
	require.Len(t, actions, 1)
	assert.Equal(t, "rdctl set --kubernetes.options.traefik=false", actions[0].Value)

	fromEvidence := portConflictActions(runtime.FlavorUnknown, runtime.FlavorRancherDesktop, "443")
	require.Len(t, fromEvidence, 1, "installed-runtime evidence must carry the hint when the socket flavor is unknown (e.g. Windows named pipe)")

	assert.Empty(t, portConflictActions(runtime.FlavorDockerDesktop, runtime.FlavorUnknown, "443"))
	assert.Empty(t, portConflictActions(runtime.FlavorRancherDesktop, runtime.FlavorUnknown, "8443"))
}
