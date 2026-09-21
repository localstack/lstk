package integration_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const snowflakeNextContainerName = "localstack-snowflake-next"

func cleanupSnowflakeNext() {
	ctx := context.Background()
	_, _ = dockerClient.ContainerRemove(ctx, snowflakeNextContainerName, client.ContainerRemoveOptions{Force: true})
}

func writeSnowflakeNextConfig(t *testing.T, hostPort string) string {
	t.Helper()
	content := fmt.Sprintf(`
[[containers]]
type = "snowflake-next"
tag  = "latest"
port = %q
`, hostPort)
	configFile := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(configFile, []byte(content), 0644))
	return configFile
}

func TestStartTypeFlagSelectsSnowflakeNextOnFirstRun(t *testing.T) {
	t.Parallel()
	e, _ := typeTestEnv(t)
	configPath := resolvedConfigPath(t, e)
	require.NoFileExists(t, configPath)

	stdout, _, _ := runLstk(t, testContext(t), t.TempDir(), e, "start", "--type", "snowflake-next", "--non-interactive")

	assert.Contains(t, stdout, "Snowflake Preview emulator selected.")
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), `type = "snowflake-next"`)
}

func TestStartTypeFlagSwitchesFromSnowflakeToPreview(t *testing.T) {
	t.Parallel()
	e, _ := typeTestEnv(t)
	configPath := resolvedConfigPath(t, e)
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0755))
	require.NoError(t, os.WriteFile(configPath, []byte("[[containers]]\ntype = \"snowflake\"  # keep me\ntag = \"latest\"\nport = \"4566\"\n"), 0644))

	stdout, _, _ := runLstk(t, testContext(t), t.TempDir(), e, "start", "--type", "snowflake-next", "--non-interactive")

	assert.Contains(t, stdout, "Switched configured emulator to Snowflake Preview")
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), `type = "snowflake-next"`)
	assert.Contains(t, string(data), "# keep me")
}

// TestFirstRunPickerOmitsSnowflakeNext pins the decision that a preview emulator
// is reachable through --type but is never offered to a first-time user: the
// picker is a new install's first impression and should only present GA products.
func TestFirstRunPickerOmitsSnowflakeNext(t *testing.T) {
	requireDocker(t)
	t.Parallel()

	tmpHome := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmpHome, ".config"), 0755))
	e := env.Environ(testEnvWithHome(tmpHome, tmpHome)).
		With(env.DisableEvents, "1")

	configPath, _, err := runLstk(t, testContext(t), "", e, "config", "path")
	require.NoError(t, err)
	require.NoFileExists(t, configPath)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	p := startLstkInPTY(t, ctx, e, "start")
	p.waitForOutput("Which emulator would you like to use?", "emulator selection prompt should appear on first run")

	// Wait for the option list to render before asserting on absence, so this
	// cannot pass merely by reading the screen too early.
	p.waitForOutput("Snowflake", "the picker should offer the GA Snowflake emulator")
	assert.NotContains(t, p.output(), "Snowflake Preview",
		"the first-run picker must not offer the preview emulator")

	p.kill()
}

// TestStartSnowflakeNextServesGatewayOnConfiguredPort is the end-to-end proof
// that the preview type needs no per-emulator adaptation: the image binds from
// GATEWAY_LISTEN like every other emulator, so the generic start path alone has
// to produce something answering the health contract on the configured host
// port. It is what would fail first if the image stopped being a drop-in.
func TestStartSnowflakeNextServesGatewayOnConfiguredPort(t *testing.T) {
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)

	cleanup()
	cleanupSnowflakeNext()
	t.Cleanup(cleanup)
	t.Cleanup(cleanupSnowflakeNext)

	const hostPort = "4577"
	configFile := writeSnowflakeNextConfig(t, hostPort)

	ctx := testContext(t)
	stdout, stderr, err := runLstk(t, ctx, "", env.Environ(testEnvWithHome(t.TempDir(), "")), "--config", configFile, "start")
	require.NoError(t, err, "lstk start failed: %s", stderr)
	requireExitCode(t, 0, err)

	inspect, err := dockerClient.ContainerInspect(ctx, snowflakeNextContainerName, client.ContainerInspectOptions{})
	require.NoError(t, err, "failed to inspect snowflake-next container")
	require.True(t, inspect.Container.State.Running, "snowflake-next container should be running")
	assert.Contains(t, inspect.Container.Config.Image, "localstack/snowflake-next",
		"expected localstack/snowflake-next image, got %s", inspect.Container.Config.Image)

	resp, err := http.Get(fmt.Sprintf("http://localhost:%s/_localstack/health", hostPort))
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	assert.Equal(t, http.StatusOK, resp.StatusCode,
		"the emulator must answer the health contract on the configured host port")

	assert.Contains(t, stdout, "• Snowflake endpoint: http://snowflake.",
		"the preview emulator should print the snowflake-prefixed endpoint hint")
}

// TestTerraformRejectsRunningSnowflakeNext covers the discovery side of the
// preview type: the IaC proxies support only the AWS emulator, and they name the
// emulator that is actually running so the error is not a misleading "AWS not
// running". That naming enumerates the known types, so a type the interactive
// picker never offers has to be included. alpine retagged as the preview image is
// enough — discovery matches on image repo and port, so no product image or
// license is needed.
func TestTerraformRejectsRunningSnowflakeNext(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)

	const fakeImage = "localstack/snowflake-next:test-fake"
	_, err := dockerClient.ImageTag(ctx, client.ImageTagOptions{Source: testImage, Target: fakeImage})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = dockerClient.ImageRemove(context.Background(), fakeImage, client.ImageRemoveOptions{})
	})
	startExternalContainer(t, ctx, fakeImage, "localstack-external-snowflake-next", "4566")

	e, _ := typeTestEnvWithDocker(t)
	stdout, _, err := runLstk(t, ctx, t.TempDir(), e, "terraform", "plan")

	require.Error(t, err)
	assert.Contains(t, stdout, "LocalStack Snowflake Preview Emulator is running",
		"the error must name the running preview emulator, not report AWS as missing")
}
