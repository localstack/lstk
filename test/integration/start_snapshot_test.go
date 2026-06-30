package integration_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- no Docker required (parallel) ---

func TestStartSnapshotConflictingFlags(t *testing.T) {
	t.Parallel()
	ctx := testContext(t)

	_, stderr, err := runLstk(t, ctx, t.TempDir(),
		testEnvWithHome(t.TempDir(), ""),
		"--non-interactive", "start", "--snapshot", "pod:my-baseline", "--no-snapshot",
	)
	requireExitCode(t, 1, err)
	assert.Contains(t, stderr, "cannot be used together")
}

func TestStartSnapshotInvalidPodName(t *testing.T) {
	t.Parallel()
	ctx := testContext(t)

	_, stderr, err := runLstk(t, ctx, t.TempDir(),
		testEnvWithHome(t.TempDir(), ""),
		"--non-interactive", "start", "--snapshot", "pod:_bad",
	)
	requireExitCode(t, 1, err)
	assert.Contains(t, stderr, "invalid pod name")
}

func TestStartSnapshotLocalFileNotFound(t *testing.T) {
	t.Parallel()
	ctx := testContext(t)

	_, stderr, err := runLstk(t, ctx, t.TempDir(),
		testEnvWithHome(t.TempDir(), ""),
		"--non-interactive", "start", "--snapshot", "/no/such/snapshot.snapshot",
	)
	requireExitCode(t, 1, err)
	assert.Contains(t, stderr, "snapshot file not found")
}

// --- Docker required ---

// TestStartAutoLoadsConfiguredSnapshot verifies that a snapshot configured in
// [[containers]].snapshot is auto-loaded once the emulator has started. The
// emulator start is real; the snapshot import is directed at a mock platform via
// LOCALSTACK_HOST so the test does not depend on a real cloud pod.
func TestStartAutoLoadsConfiguredSnapshot(t *testing.T) {
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)

	cleanup()
	t.Cleanup(cleanup)

	licenseServer := createMockLicenseServer(true)
	defer licenseServer.Close()
	podServer := mockPodLoadServer(t, true)

	configContent := `
[[containers]]
type = "aws"
tag = "latest"
port = "4566"
snapshot = "pod:my-baseline"
`
	configFile := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0644))

	ctx := testContext(t)
	stdout, stderr, err := runLstk(t, ctx, "",
		env.With(env.APIEndpoint, licenseServer.URL).With(env.LocalStackHost, lsHost(podServer)),
		"--config", configFile, "start",
	)
	require.NoError(t, err, "lstk start failed: %s", stderr)
	requireExitCode(t, 0, err)
	assert.Contains(t, stdout, "Snapshot loaded", "configured snapshot should auto-load after start")
	assert.Contains(t, stdout, "my-baseline")
}

// TestStartNoSnapshotSkipsAutoLoad verifies that --no-snapshot skips auto-loading
// the configured snapshot for that run while still starting the emulator.
func TestStartNoSnapshotSkipsAutoLoad(t *testing.T) {
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)

	cleanup()
	t.Cleanup(cleanup)

	licenseServer := createMockLicenseServer(true)
	defer licenseServer.Close()

	configContent := `
[[containers]]
type = "aws"
tag = "latest"
port = "4566"
snapshot = "pod:my-baseline"
`
	configFile := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0644))

	ctx := testContext(t)
	stdout, stderr, err := runLstk(t, ctx, "",
		env.With(env.APIEndpoint, licenseServer.URL),
		"--config", configFile, "start", "--no-snapshot",
	)
	require.NoError(t, err, "lstk start --no-snapshot failed: %s", stderr)
	requireExitCode(t, 0, err)
	assert.NotContains(t, stdout, "Snapshot loaded", "--no-snapshot should skip auto-loading")
}
