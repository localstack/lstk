package config

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolvedEnv_ResolvesNamedEnvs(t *testing.T) {
	c := &ContainerConfig{
		Env: []string{"test", "debug"},
	}
	namedEnvs := map[string]map[string]string{
		"test":  {"iam_soft_mode": "1"},
		"debug": {"ls_log": "trace", "debug": "1"},
	}

	resolved, err := c.ResolvedEnv(namedEnvs)
	require.NoError(t, err)

	sort.Strings(resolved)
	assert.Equal(t, []string{"DEBUG=1", "IAM_SOFT_MODE=1", "LS_LOG=trace"}, resolved)
}

func TestResolvedEnv_KeysAreUppercased(t *testing.T) {
	// Viper lowercases all config keys internally; ResolvedEnv must restore them.
	c := &ContainerConfig{Env: []string{"test"}}
	namedEnvs := map[string]map[string]string{
		"test": {"iam_soft_mode": "1"},
	}

	resolved, err := c.ResolvedEnv(namedEnvs)
	require.NoError(t, err)
	assert.Equal(t, []string{"IAM_SOFT_MODE=1"}, resolved)
}

func TestResolvedEnv_ErrorOnMissingEnv(t *testing.T) {
	c := &ContainerConfig{Env: []string{"missing"}}
	_, err := c.ResolvedEnv(map[string]map[string]string{})
	assert.ErrorContains(t, err, `"missing"`)
}

func TestResolvedEnv_EmptyWhenNoEnvRefs(t *testing.T) {
	c := &ContainerConfig{}
	resolved, err := c.ResolvedEnv(map[string]map[string]string{})
	require.NoError(t, err)
	assert.Empty(t, resolved)
}

func TestValidate_ValidPort(t *testing.T) {
	c := &ContainerConfig{Type: EmulatorAWS, Port: "4566"}
	assert.NoError(t, c.Validate())
}

func TestValidate_MinMaxPorts(t *testing.T) {
	c := &ContainerConfig{Type: EmulatorAWS, Port: "1"}
	assert.NoError(t, c.Validate())

	c.Port = "65535"
	assert.NoError(t, c.Validate())
}

func TestValidate_EmptyPort(t *testing.T) {
	c := &ContainerConfig{Type: EmulatorAWS, Port: ""}
	err := c.Validate()
	assert.ErrorContains(t, err, "port is required")
}

func TestValidate_NonNumericPort(t *testing.T) {
	c := &ContainerConfig{Type: EmulatorAWS, Port: "abc"}
	err := c.Validate()
	assert.ErrorContains(t, err, "not a valid number")
}

func TestValidate_PortZero(t *testing.T) {
	c := &ContainerConfig{Type: EmulatorAWS, Port: "0"}
	err := c.Validate()
	assert.ErrorContains(t, err, "out of range")
}

func TestValidate_PortTooHigh(t *testing.T) {
	c := &ContainerConfig{Type: EmulatorAWS, Port: "65536"}
	err := c.Validate()
	assert.ErrorContains(t, err, "out of range")
}

func TestValidate_NegativePort(t *testing.T) {
	c := &ContainerConfig{Type: EmulatorAWS, Port: "-1"}
	err := c.Validate()
	assert.ErrorContains(t, err, "out of range")
}
