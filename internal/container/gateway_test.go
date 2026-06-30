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
	extra := g.extraGatewayPorts("4566")
	require.Len(t, extra, 2)
	assert.Equal(t, "443", extra[0].ContainerPort)
	assert.Equal(t, "443", extra[0].HostPort)
	assert.Equal(t, "8443", extra[1].ContainerPort)
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
