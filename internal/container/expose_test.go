package container

import (
	"context"
	"errors"
	"testing"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// autoPublished is what lstk publishes on its own for an AWS emulator on the
// default port: the gateway's extra 443 plus a couple of service-range ports
// (the full 4510-4559 range is irrelevant to these cases).
func autoPublished() []runtime.PortMapping {
	return []runtime.PortMapping{
		{ContainerPort: "443", HostPort: "443", Optional: true},
		{ContainerPort: "4510", HostPort: "4510"},
		{ContainerPort: "4511", HostPort: "4511"},
	}
}

func exposedFor(t *testing.T, entries ...string) []config.PortSpec {
	t.Helper()
	c := &config.ContainerConfig{Type: config.EmulatorAWS, Port: "4566", ExposePorts: entries}
	specs, err := c.ExposedPorts()
	require.NoError(t, err)
	return specs
}

func TestMergeExposePortsPublishesRequestedPorts(t *testing.T) {
	sink := &recordingSink{}

	merged := mergeExposePorts(sink, autoPublished(), "4566", "4566", exposedFor(t, "53"))

	// expose_ports is an explicit request, so the mappings are never Optional:
	// a busy or unbindable host port must fail the start, not be dropped.
	assert.Equal(t, []runtime.PortMapping{
		{ContainerPort: "53", HostPort: "53", Protocol: "tcp"},
		{ContainerPort: "53", HostPort: "53", Protocol: "udp"},
	}, merged[len(autoPublished()):])
	assert.Empty(t, sink.messageTexts())
}

func TestMergeExposePortsRemapsHostPort(t *testing.T) {
	sink := &recordingSink{}

	merged := mergeExposePorts(sink, nil, "4566", "4566", exposedFor(t, "5354:53/udp"))

	assert.Equal(t, []runtime.PortMapping{
		{ContainerPort: "53", HostPort: "5354", Protocol: "udp"},
	}, merged)
	assert.Empty(t, sink.messageTexts())
}

func TestMergeExposePortsSkipsRedundantEntriesSilently(t *testing.T) {
	sink := &recordingSink{}

	// Asking for exactly what lstk already publishes changes nothing, so there is
	// nothing to warn about.
	merged := mergeExposePorts(sink, autoPublished(), "4566", "4566", exposedFor(t, "443/tcp", "4510:4510/tcp"))

	assert.Equal(t, autoPublished(), merged)
	assert.Empty(t, sink.messageTexts())
}

func TestMergeExposePortsWarnsWhenLstkAlreadyPublishesTheContainerPort(t *testing.T) {
	sink := &recordingSink{}

	// The automatic mapping wins, so the requested host port would never take
	// effect — say so instead of dropping it silently.
	merged := mergeExposePorts(sink, autoPublished(), "4566", "4566", exposedFor(t, "8443:443/tcp"))

	assert.Equal(t, autoPublished(), merged)
	require.Len(t, sink.messageTexts(), 1)
	assert.Contains(t, sink.messageTexts()[0], "already publishes container port 443/tcp on host port 443")
}

func TestMergeExposePortsWarnsWhenHostPortIsTakenByTheEdgePort(t *testing.T) {
	sink := &recordingSink{}

	merged := mergeExposePorts(sink, autoPublished(), "4566", "4566", exposedFor(t, "4566:53/tcp"))

	assert.Equal(t, autoPublished(), merged)
	require.Len(t, sink.messageTexts(), 1)
	assert.Contains(t, sink.messageTexts()[0], "host port 4566/tcp is already used to publish container port 4566")
}

func TestRequiredHostPortsExcludesUDP(t *testing.T) {
	// The preflight dials TCP, so a UDP publication cannot be checked that way —
	// including it would report a phantom conflict whenever the same port number is
	// held by an unrelated TCP listener.
	required := requiredHostPorts([]runtime.PortMapping{
		{ContainerPort: "443", HostPort: "443", Optional: true},
		{ContainerPort: "4510", HostPort: "4510"},
		{ContainerPort: "53", HostPort: "53", Protocol: "tcp"},
		{ContainerPort: "53", HostPort: "53", Protocol: "udp"},
	})

	assert.Equal(t, []string{"4510", "53"}, required)
}

// TestStartFailureOnRequiredPortBindCarriesRuntimeHint covers the sub-1024 case
// expose_ports makes reachable: a required port the daemon may refuse to bind.
func TestStartFailureOnRequiredPortBindCarriesRuntimeHint(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)

	c := runtime.ContainerConfig{
		Name:       "localstack-aws",
		Port:       "4566",
		ExtraPorts: []runtime.PortMapping{{ContainerPort: "53", HostPort: "53", Protocol: "udp"}},
	}
	bindErr := errors.New(`Error response from daemon: driver failed programming external connectivity: listen tcp 127.0.0.1:53: bind: permission denied`)

	mockRT.EXPECT().Start(gomock.Any(), c).Return("", nil, bindErr)
	mockRT.EXPECT().Flavor().Return(runtime.FlavorPodman)

	_, _, err := startWithOptionalPortFallback(context.Background(), mockRT, &recordingSink{}, c)
	require.Error(t, err)
	assert.ErrorIs(t, err, bindErr, "the daemon error must stay the cause — a required port is never dropped")
	assert.Contains(t, err.Error(), "podman machine set --rootful")
}

func TestMergeExposePortsAllowsUDPOnAnAlreadyPublishedTCPPort(t *testing.T) {
	sink := &recordingSink{}

	// 4510/tcp being published says nothing about 4510/udp.
	merged := mergeExposePorts(sink, autoPublished(), "4566", "4566", exposedFor(t, "4510/udp"))

	assert.Equal(t, []runtime.PortMapping{
		{ContainerPort: "4510", HostPort: "4510", Protocol: "udp"},
	}, merged[len(autoPublished()):])
	assert.Empty(t, sink.messageTexts())
}
