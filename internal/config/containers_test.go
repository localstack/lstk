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
