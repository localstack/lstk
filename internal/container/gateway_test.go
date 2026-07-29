package container

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseGatewayListenDefaults(t *testing.T) {
	for _, value := range []string{"", "   "} {
		g, err := parseGatewayListen(value)
		require.NoError(t, err)
		assert.Equal(t, "127.0.0.1", g.bindHost())
		assert.Equal(t, ":4566,:443", g.containerEnvValue())
	}
}

func TestParseGatewayListenExternalHost(t *testing.T) {
	g, err := parseGatewayListen("0.0.0.0:4566,0.0.0.0:443")
	require.NoError(t, err)
	assert.Equal(t, "0.0.0.0", g.bindHost())
	// Non-loopback hosts are preserved in the container env value.
	assert.Equal(t, "0.0.0.0:4566,0.0.0.0:443", g.containerEnvValue())
}

func TestParseGatewayListenLoopbackHostIsBlankedForContainer(t *testing.T) {
	g, err := parseGatewayListen("127.0.0.1:4566,127.0.0.1:443")
	require.NoError(t, err)
	// Bind host is loopback, but the container listens on all interfaces.
	assert.Equal(t, "127.0.0.1", g.bindHost())
	assert.Equal(t, ":4566,:443", g.containerEnvValue())
}

func TestParseGatewayListenExtraPortsPublished(t *testing.T) {
	g, err := parseGatewayListen("0.0.0.0:4566,0.0.0.0:443,0.0.0.0:8443")
	require.NoError(t, err)
	extra := g.extraGatewayPorts("4566", false)
	require.Len(t, extra, 2)
	assert.Equal(t, "443", extra[0].ContainerPort)
	assert.Equal(t, "443", extra[0].HostPort)
	assert.False(t, extra[0].Optional)
	assert.Equal(t, "8443", extra[1].ContainerPort)
}

func TestExtraGatewayPortsMarkedOptionalWhenDefaulted(t *testing.T) {
	g, err := parseGatewayListen("")
	require.NoError(t, err)
	extra := g.extraGatewayPorts("4566", true)
	require.Len(t, extra, 1)
	assert.Equal(t, "443", extra[0].HostPort)
	assert.True(t, extra[0].Optional)
}

func TestParseGatewayListenBarePort(t *testing.T) {
	g, err := parseGatewayListen("4566")
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1", g.bindHost())
	assert.Equal(t, ":4566", g.containerEnvValue())
}

func TestParseGatewayListenInvalid(t *testing.T) {
	for _, value := range []string{"notaport", ":abc", "999.999.999.999:4566"} {
		_, err := parseGatewayListen(value)
		assert.Error(t, err, "expected error for %q", value)
	}
}

func TestParseGatewayListenExtraPortsSkipsServicePortRange(t *testing.T) {
	g, err := parseGatewayListen("0.0.0.0:4566,0.0.0.0:443,0.0.0.0:4520,0.0.0.0:8443")
	require.NoError(t, err)
	extra := g.extraGatewayPorts("4566", false)
	require.Len(t, extra, 2, "the 4520 entry already falls in the service port range and must not be duplicated")
	assert.Equal(t, "443", extra[0].ContainerPort)
	assert.Equal(t, "8443", extra[1].ContainerPort)
}

func TestParseGatewayListenBracketedIPv6Host(t *testing.T) {
	g, err := parseGatewayListen("[::]:4566,[::]:443")
	require.NoError(t, err)
	assert.Equal(t, "::", g.bindHost())
	assert.Equal(t, "[::]:4566,[::]:443", g.containerEnvValue())
}

func TestParseGatewayListenUnbracketedIPv6HostErrors(t *testing.T) {
	// Unbracketed IPv6 is ambiguous with the host:port separator, so it must
	// be rejected rather than mis-parsed.
	_, err := parseGatewayListen("::1:4566")
	assert.Error(t, err)
}
