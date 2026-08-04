package integration_test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigFlagEnvVarsPassedToContainer(t *testing.T) {
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)

	cleanup()
	t.Cleanup(cleanup)

	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	configContent := `
[[containers]]
type = "aws"
tag = "latest"
port = "4566"
env = ["test"]

[env.test]
IAM_SOFT_MODE = "1"
`
	configFile := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0644))

	ctx := testContext(t)
	_, stderr, err := runLstk(t, ctx, "", env.With(env.APIEndpoint, mockServer.URL), "--config", configFile, "start")
	require.NoError(t, err, "lstk start failed: %s", stderr)
	requireExitCode(t, 0, err)

	inspect, err := dockerClient.ContainerInspect(ctx, containerName, client.ContainerInspectOptions{})
	require.NoError(t, err, "failed to inspect container")
	assert.Contains(t, inspect.Container.Config.Env, "IAM_SOFT_MODE=1")
}

func testEnvWithHome(tmpHome, xdgConfigHome string) []string {
	e := env.Without("HOME", "XDG_CONFIG_HOME", "APPDATA", "USERPROFILE", "HOMEDRIVE", "HOMEPATH")
	switch runtime.GOOS {
	case "darwin", "linux":
		e = append(e, "HOME="+tmpHome, "XDG_CONFIG_HOME="+xdgConfigHome, fmt.Sprintf("%s=file", env.Keyring))
	case "windows":
		appData := filepath.Join(tmpHome, "AppData", "Roaming")
		e = append(e, "HOME="+tmpHome, "USERPROFILE="+tmpHome, "APPDATA="+appData, fmt.Sprintf("%s=file", env.Keyring))
	default:
		panic("unsupported OS: " + runtime.GOOS)
	}
	return e
}

func writeConfigFile(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	content := "[[containers]]\ntype = \"aws\"\ntag = \"latest\"\nport = \"4566\"\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
}
