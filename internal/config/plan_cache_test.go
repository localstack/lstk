package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func setupCacheTest(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	resolved, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)

	configFile := filepath.Join(resolved, configFileName)
	require.NoError(t, os.WriteFile(configFile, []byte("[aws]\n"), 0600))

	viper.Reset()
	viper.SetConfigFile(configFile)
	require.NoError(t, viper.ReadInConfig())

	return resolved
}

func TestCachePlanLabelRoundTrip(t *testing.T) {
	dir := setupCacheTest(t)

	CachePlanLabel("LocalStack Ultimate")

	data, err := os.ReadFile(filepath.Join(dir, planCacheFile))
	require.NoError(t, err)
	require.Equal(t, "LocalStack Ultimate\n", string(data))

	got := CachedPlanLabel()
	require.Equal(t, "LocalStack Ultimate", got)
}

func TestCachedPlanLabelReturnsEmptyWhenNoFile(t *testing.T) {
	setupCacheTest(t)

	got := CachedPlanLabel()
	require.Equal(t, "", got)
}

func TestCachePlanLabelOverwritesPrevious(t *testing.T) {
	setupCacheTest(t)

	CachePlanLabel("LocalStack Pro")
	CachePlanLabel("LocalStack Ultimate")

	got := CachedPlanLabel()
	require.Equal(t, "LocalStack Ultimate", got)
}
