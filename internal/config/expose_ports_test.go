package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExposedPorts_BarePortCoversBothProtocols(t *testing.T) {
	// A bare port expands to tcp and udp so `expose_ports = [53]` is enough for the
	// emulator's DNS server, which serves queries over both.
	c := &ContainerConfig{Type: EmulatorAWS, Port: "4566", ExposePorts: []string{"53"}}

	specs, err := c.ExposedPorts()
	require.NoError(t, err)
	assert.Equal(t, []PortSpec{
		{HostPort: "53", ContainerPort: "53", Protocol: "tcp"},
		{HostPort: "53", ContainerPort: "53", Protocol: "udp"},
	}, specs)
}

func TestExposedPorts_Forms(t *testing.T) {
	tests := []struct {
		entry string
		want  []PortSpec
	}{
		{"53/udp", []PortSpec{{HostPort: "53", ContainerPort: "53", Protocol: "udp"}}},
		{"5354:53/udp", []PortSpec{{HostPort: "5354", ContainerPort: "53", Protocol: "udp"}}},
		{"53/UDP", []PortSpec{{HostPort: "53", ContainerPort: "53", Protocol: "udp"}}},
		{"9000:9000/tcp", []PortSpec{{HostPort: "9000", ContainerPort: "9000", Protocol: "tcp"}}},
		{" 25 ", []PortSpec{
			{HostPort: "25", ContainerPort: "25", Protocol: "tcp"},
			{HostPort: "25", ContainerPort: "25", Protocol: "udp"},
		}},
		{"0053", []PortSpec{
			{HostPort: "53", ContainerPort: "53", Protocol: "tcp"},
			{HostPort: "53", ContainerPort: "53", Protocol: "udp"},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.entry, func(t *testing.T) {
			c := &ContainerConfig{Type: EmulatorAWS, Port: "4566", ExposePorts: []string{tt.entry}}
			specs, err := c.ExposedPorts()
			require.NoError(t, err)
			assert.Equal(t, tt.want, specs)
		})
	}
}

func TestExposedPorts_DeduplicatesIdenticalPublications(t *testing.T) {
	c := &ContainerConfig{Type: EmulatorAWS, Port: "4566", ExposePorts: []string{"53", "53/udp", "53/tcp"}}

	specs, err := c.ExposedPorts()
	require.NoError(t, err)
	assert.Len(t, specs, 2)
}

func TestExposedPorts_InvalidEntries(t *testing.T) {
	tests := []struct {
		name    string
		entries []string
		wantErr string
	}{
		{"empty", []string{""}, "entry is empty"},
		{"non-numeric", []string{"dns"}, "not a valid port number"},
		{"non-numeric host", []string{"dns:53"}, "not a valid port number"},
		{"zero", []string{"0"}, "out of range"},
		{"too high", []string{"65536"}, "out of range"},
		{"unknown protocol", []string{"53/sctp"}, `protocol must be "tcp" or "udp"`},
		{"too many colons", []string{"1:2:3"}, "expected"},
		{"same container port, two host ports", []string{"53/udp", "5354:53/udp"}, "container port 53/udp is published on both"},
		{"same host port, two container ports", []string{"1053:53/udp", "1053:54/udp"}, "host port 1053/udp is claimed by both"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &ContainerConfig{Type: EmulatorAWS, Port: "4566", ExposePorts: tt.entries}
			_, err := c.ExposedPorts()
			assert.ErrorContains(t, err, tt.wantErr)
			// A bad entry must fail at config load, not at container creation.
			assert.ErrorContains(t, c.Validate(), tt.wantErr)
		})
	}
}

// TestGet_ExposePortsAcceptsIntsAndStrings pins the TOML surface promised in the
// docs: numbers and strings may be mixed in the same list.
func TestGet_ExposePortsAcceptsIntsAndStrings(t *testing.T) {
	// Cannot run in parallel: mutates process-wide viper state.
	configFile := filepath.Join(t.TempDir(), configFileName)
	require.NoError(t, os.WriteFile(configFile, []byte(`
[[containers]]
type = "aws"
port = "4566"
expose_ports = [53, "5354:5353/udp", 9000]
`), 0600))

	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.SetConfigFile(configFile)
	require.NoError(t, viper.ReadInConfig())

	cfg, err := Get()
	require.NoError(t, err)
	require.Len(t, cfg.Containers, 1)
	assert.Equal(t, []string{"53", "5354:5353/udp", "9000"}, cfg.Containers[0].ExposePorts)
}
