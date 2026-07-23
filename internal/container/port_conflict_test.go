package container

import (
	"net"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	withHint := optionalPortDropWarning(runtime.FlavorRancherDesktop, "443", "4566")
	assert.Contains(t, withHint, "rdctl set --kubernetes.options.traefik=false")

	withoutHint := optionalPortDropWarning(runtime.FlavorDockerDesktop, "443", "4566")
	assert.NotContains(t, withoutHint, "rdctl")
}

func TestPortConflictActions(t *testing.T) {
	actions := portConflictActions(runtime.FlavorRancherDesktop, "443")
	require.Len(t, actions, 1)
	assert.Equal(t, "rdctl set --kubernetes.options.traefik=false", actions[0].Value)

	assert.Empty(t, portConflictActions(runtime.FlavorDockerDesktop, "443"))
	assert.Empty(t, portConflictActions(runtime.FlavorRancherDesktop, "8443"))
}
