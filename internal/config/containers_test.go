package config

import (
	"sort"
	"strings"
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

func TestValidate_ZeroPaddedMonthTag_IsRejected(t *testing.T) {
	for _, tag := range []string{"2026.04", "2026.04.1", "2026.04.0-amd64", "2026.01", "2026.09.2"} {
		t.Run(tag, func(t *testing.T) {
			c := &ContainerConfig{Type: EmulatorAWS, Port: "4566", Tag: tag}
			assert.ErrorContains(t, c.Validate(), "unsupported")
		})
	}
}

func TestValidate_InvalidDockerTag_IsRejected(t *testing.T) {
	for _, tag := range []string{
		"my tag",   // space
		"2026.4!",  // special char
		".hidden",  // starts with dot
		"-beta",    // starts with hyphen
		"tag@sha",  // @ not allowed
		"foo:bar",  // colon not allowed
		strings.Repeat("a", 129), // too long
	} {
		t.Run(tag, func(t *testing.T) {
			c := &ContainerConfig{Type: EmulatorAWS, Port: "4566", Tag: tag}
			err := c.Validate()
			assert.ErrorContains(t, err, "unsupported")
		})
	}
}

func TestValidate_ValidTagFormats_AreAccepted(t *testing.T) {
	for _, tag := range []string{
		"", "latest", "stable",
		"2026.4", "2026.4.1", "2026.4.0", "2026.4.0-amd64", "2026.4.0-arm64",
		"2026.5.0.dev188",
		"2026.10", "2026.11.2",
		"3.8.0", "3.7.4",
	} {
		t.Run(tag, func(t *testing.T) {
			c := &ContainerConfig{Type: EmulatorAWS, Port: "4566", Tag: tag}
			assert.NoError(t, c.Validate())
		})
	}
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
