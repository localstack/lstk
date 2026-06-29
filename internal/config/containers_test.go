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

func TestValidate_ZeroPaddedMonthTag_IsAccepted(t *testing.T) {
	for _, tag := range []string{"2026.04", "2026.04.1", "2026.04.0-amd64", "2026.01", "2026.09.2"} {
		t.Run(tag, func(t *testing.T) {
			c := &ContainerConfig{Type: EmulatorAWS, Port: "4566", Tag: tag}
			assert.NoError(t, c.Validate())
		})
	}
}

func TestNormalizeTag(t *testing.T) {
	for _, tc := range []struct {
		input, want string
	}{
		{"2026.04", "2026.4"},
		{"2026.01", "2026.1"},
		{"2026.09.2", "2026.9.2"},
		{"2026.04.1", "2026.4.1"},
		{"2026.04.0-amd64", "2026.4.0-amd64"},
		{"2026.10", "2026.10"},
		{"latest", "latest"},
		{"", ""},
	} {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.want, NormalizeTag(tc.input))
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

func TestAzureEmulatorResolvesStartMetadata(t *testing.T) {
	c := &ContainerConfig{Type: EmulatorAzure, Port: "4566"}

	image, err := c.Image()
	require.NoError(t, err)
	assert.Equal(t, "localstack/localstack-azure:latest", image)

	productName, err := c.ProductName()
	require.NoError(t, err)
	assert.Equal(t, "localstack-azure", productName)

	healthPath, err := c.HealthPath()
	require.NoError(t, err)
	assert.Equal(t, "/_localstack/health", healthPath)

	containerPort, err := c.ContainerPort()
	require.NoError(t, err)
	assert.Equal(t, "4566/tcp", containerPort)
}

func TestEmulatorTypeForImage_Azure(t *testing.T) {
	assert.Equal(t, EmulatorAzure, EmulatorTypeForImage("localstack/localstack-azure:latest"))
}

func TestImage_CustomImage(t *testing.T) {
	tests := []struct {
		name        string
		customImage string
		tag         string
		want        string
	}{
		{"untagged custom image gets configured tag", "my-registry.internal/localstack-pro", "2026.4", "my-registry.internal/localstack-pro:2026.4"},
		{"untagged custom image defaults to latest", "local-image-name", "", "local-image-name:latest"},
		{"tagged custom image is used as-is", "my-registry.internal/localstack-pro:custom", "latest", "my-registry.internal/localstack-pro:custom"},
		{"registry port is not mistaken for a tag", "my-registry:5000/localstack-pro", "2026.4", "my-registry:5000/localstack-pro:2026.4"},
		{"registry port with explicit tag", "my-registry:5000/localstack-pro:custom", "", "my-registry:5000/localstack-pro:custom"},
		{"digest-pinned image is used as-is", "localstack/localstack-pro@sha256:abc123def456", "2026.4", "localstack/localstack-pro@sha256:abc123def456"},
		{"registry port with digest is used as-is", "my-registry:5000/localstack-pro@sha256:abc123def456", "", "my-registry:5000/localstack-pro@sha256:abc123def456"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &ContainerConfig{Type: EmulatorAWS, Port: "4566", Tag: tt.tag, CustomImage: tt.customImage}
			image, err := c.Image()
			require.NoError(t, err)
			assert.Equal(t, tt.want, image)
		})
	}
}

func TestImage_DefaultWhenNoCustomImage(t *testing.T) {
	c := &ContainerConfig{Type: EmulatorAWS, Port: "4566", Tag: "latest"}
	image, err := c.Image()
	require.NoError(t, err)
	assert.Equal(t, "localstack/localstack-pro:latest", image)
}

func TestSelfValidatesLicense(t *testing.T) {
	// Snowflake and Azure containers activate their own license against the
	// licensing server, so lstk skips its pre-flight platform license check.
	assert.True(t, EmulatorSnowflake.SelfValidatesLicense())
	assert.True(t, EmulatorAzure.SelfValidatesLicense())
	assert.False(t, EmulatorAWS.SelfValidatesLicense())
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
