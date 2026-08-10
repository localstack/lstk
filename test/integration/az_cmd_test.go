package integration_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAzCommandFailsWhenAzureCLINotInstalled(t *testing.T) {
	t.Parallel()
	workDir := azureWorkDir(t)
	writeAzureSetupMarker(t, workDir)

	e := env.With(env.DisableEvents, "1").With("PATH", t.TempDir()).WithHome(t.TempDir())

	stdout, _, err := runLstk(t, testContext(t), workDir, e, "az", "group", "list")
	require.Error(t, err)
	assert.Contains(t, stdout, "az CLI not found in PATH")
	assert.Contains(t, stdout, "Install Azure CLI:")
	assert.Contains(t, stdout, "https://learn.microsoft.com/en-us/cli/azure/")
}

// azureHealthServer answers like an Azure-flavored emulator: /_localstack/health
// omits "version" (which is what makes type detection fall back to
// /_localstack/info), so a --endpoint-url pointed here is detected as azure and
// `lstk az` never touches Docker.
func azureHealthServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/_localstack/health":
			_ = json.NewEncoder(w).Encode(map[string]any{"services": map[string]string{}})
		case "/_localstack/info":
			_ = json.NewEncoder(w).Encode(map[string]any{"version": "3.0.2"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// writeFakeAz creates a fake `az` that prints its args, sleeping first when
// sleepSeconds > 0 so PTY-based tests have time to observe the spinner.
// Returns the directory containing it (to prepend to PATH).
func writeFakeAz(t *testing.T, sleepSeconds int) string {
	t.Helper()
	return writeFakeTool(t, "az", fakeToolConfig{
		SleepSeconds: sleepSeconds,
		Stdout:       []string{"AZ_ARGS:{args}"},
	})
}

// azureConfigWithSetupMarker writes a config.toml holding an Azure container at
// a path *outside* any config search dir, plus the azure setup marker next to
// it (azureconfig.ConfigDir is derived from the resolved config file's
// directory). `lstk az` can only find both if --config actually reached config
// resolution, which makes the returned path a probe for that.
func azureConfigWithSetupMarker(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(configPath, []byte(`
[[containers]]
type = "azure"
tag  = "latest"
port = "4566"
`), 0644))
	markerDir := filepath.Join(dir, "azure")
	require.NoError(t, os.MkdirAll(markerDir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(markerDir, ".lstk-setup-complete"), []byte("ok\n"), 0600))
	return configPath
}

// azFakeEnv is the environment for the fake-`az` tests below: DOCKER_HOST points
// at a nonexistent socket so any code path that fell back to Docker discovery
// would fail loudly instead of silently passing on a developer machine.
func azFakeEnv(t *testing.T, fakeDir, homeDir string) []string {
	t.Helper()
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(homeDir)
	return append(e, unreachableDockerHost)
}

// azureHomeWithSetupMarker prepares a HOME whose $HOME/.config/lstk holds both
// an Azure config.toml and the azure setup marker, so `lstk az` is fully set up
// without a --config flag. runLstkInPTY runs with no working directory of its
// own, so the PTY tests below can't use a project-local config.
func azureHomeWithSetupMarker(t *testing.T) string {
	t.Helper()
	homeDir := t.TempDir()
	lstkDir := filepath.Join(homeDir, ".config", "lstk")
	require.NoError(t, os.MkdirAll(filepath.Join(lstkDir, "azure"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(lstkDir, "config.toml"), []byte(`
[[containers]]
type = "azure"
tag  = "latest"
port = "4566"
`), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(lstkDir, "azure", ".lstk-setup-complete"), []byte("ok\n"), 0600))
	return homeDir
}

// `lstk az` sets DisableFlagParsing, so lstk's own persistent flags must be
// stripped from the forwarded args in PreRunE — otherwise they reach the `az`
// child, which rejects them as unknown, and their effect on lstk is lost.
func TestAzCommandStripsGlobalFlagsFromPassthrough(t *testing.T) {
	t.Parallel()
	srv := azureHealthServer(t)
	configPath := azureConfigWithSetupMarker(t)

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), azFakeEnv(t, writeFakeAz(t, 0), t.TempDir()),
		"--endpoint-url", srv.URL, "--config", configPath, "--non-interactive", "az", "group", "list")
	require.NoError(t, err, "stderr: %s", stderr)

	assert.Contains(t, stdout, "AZ_ARGS:group list")
	assert.NotContains(t, stdout, "--non-interactive")
	assert.NotContains(t, stdout, "--config")
}

// --config must select the given config file for `lstk az` too: the Azure setup
// marker lives next to that file, so the passthrough only runs if the flag was
// honored. The no-flag run is the control — same environment, no config file
// found, so the setup check fails instead.
func TestAzCommandConfigFlagSelectsConfigFile(t *testing.T) {
	t.Parallel()
	srv := azureHealthServer(t)
	configPath := azureConfigWithSetupMarker(t)
	e := azFakeEnv(t, writeFakeAz(t, 0), t.TempDir())

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e,
		"--endpoint-url", srv.URL, "--config", configPath, "az", "group", "list")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Contains(t, stdout, "AZ_ARGS:group list")

	stdout, _, err = runLstk(t, testContext(t), t.TempDir(), e,
		"--endpoint-url", srv.URL, "az", "group", "list")
	require.Error(t, err, "without --config the azure setup marker must not be found")
	assert.Contains(t, stdout, "Azure CLI integration is not set up")
}

func TestAzCommandShowsSpinnerForSlowOperation(t *testing.T) {
	t.Parallel()
	srv := azureHealthServer(t)

	// The spinner only renders after 4s, so the fake `az` must outlast that.
	out, err := runLstkInPTY(t, testContext(t), azFakeEnv(t, writeFakeAz(t, 5), azureHomeWithSetupMarker(t)),
		"--endpoint-url", srv.URL, "az", "group", "list")
	require.NoError(t, err, "lstk az failed: %s", out)

	assert.Contains(t, out, "Loading service")
	assert.Contains(t, out, "AZ_ARGS:group list")
}

// --non-interactive must suppress the spinner: before the flag was stripped in
// PreRunE it never reached cfg.NonInteractive, leaving the guard inert and ANSI
// control codes in the captured streams.
func TestAzCommandSuppressesSpinnerInNonInteractiveMode(t *testing.T) {
	t.Parallel()
	srv := azureHealthServer(t)

	out, err := runLstkInPTY(t, testContext(t), azFakeEnv(t, writeFakeAz(t, 5), azureHomeWithSetupMarker(t)),
		"--endpoint-url", srv.URL, "--non-interactive", "az", "group", "list")
	require.NoError(t, err, "lstk az failed: %s", out)

	assert.NotContains(t, out, "Loading service")
	assert.Contains(t, out, "AZ_ARGS:group list")
}
