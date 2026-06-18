package integration_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// save/load work for Snowflake and Azure but emit an experimental warning.
// These tests pin that warning (and its absence for AWS).

const experimentalWarningFragment = "experimental and not fully tested"

// nonAWSEmulator describes a non-AWS emulator under test: its config writer, its
// container starter/cleanup, and the emulator name expected in the warning.
type nonAWSEmulator struct {
	name           string // ShortName as it appears in the warning, e.g. "Snowflake"
	writeConfig    func(t *testing.T, hostPort string) string
	startContainer func(t *testing.T, ctx context.Context)
	cleanup        func()
}

func nonAWSEmulators() []nonAWSEmulator {
	return []nonAWSEmulator{
		{
			name:           "Snowflake",
			writeConfig:    writeSnowflakeConfig,
			startContainer: startTestSnowflakeContainer,
			cleanup:        cleanupSnowflake,
		},
		{
			name:           "Azure",
			writeConfig:    writeAzureConfig,
			startContainer: startTestAzureContainer,
			cleanup:        cleanupAzure,
		},
	}
}

// snapshotOp describes one snapshot operation (save/load, local/pod) under test.
// setup builds the mock LocalStack server and the snapshot subcommand args for a
// run whose working directory is dir; withAuth selects whether an auth token is
// supplied (pod operations are cloud-only and require it).
type snapshotOp struct {
	name        string
	successText string
	withAuth    bool
	setup       func(t *testing.T, dir string) (srv *httptest.Server, subArgs []string)
}

func snapshotOps() []snapshotOp {
	return []snapshotOp{
		{
			name:        "Save",
			successText: "Snapshot saved",
			setup: func(t *testing.T, dir string) (*httptest.Server, []string) {
				return mockStateServer(t), []string{"snapshot", "save"}
			},
		},
		{
			name:        "SavePod",
			successText: "Snapshot saved",
			withAuth:    true,
			setup: func(t *testing.T, dir string) (*httptest.Server, []string) {
				return mockPodSaveServer(t, true), []string{"snapshot", "save", "pod:my-baseline"}
			},
		},
		{
			name:        "Load",
			successText: "Snapshot loaded",
			setup: func(t *testing.T, dir string) (*httptest.Server, []string) {
				srv, _ := mockLocalLoadServer(t)
				snapPath := writeTestSnapFile(t, dir, "snap.snapshot")
				return srv, []string{"snapshot", "load", snapPath}
			},
		},
		{
			name:        "LoadPod",
			successText: "Snapshot loaded",
			withAuth:    true,
			setup: func(t *testing.T, dir string) (*httptest.Server, []string) {
				return mockPodLoadServer(t, true), []string{"snapshot", "load", "pod:my-baseline"}
			},
		},
	}
}

func TestSnapshotNonAWSShowsExperimentalWarning(t *testing.T) {
	requireDocker(t)

	for _, op := range snapshotOps() {
		for _, em := range nonAWSEmulators() {
			t.Run(op.name+"/"+em.name, func(t *testing.T) {
				cleanup()
				em.cleanup()
				t.Cleanup(cleanup)
				t.Cleanup(em.cleanup)

				ctx := testContext(t)
				em.startContainer(t, ctx)
				dir := t.TempDir()
				srv, subArgs := op.setup(t, dir)

				environ := env.Environ(testEnvWithHome(t.TempDir(), "")).With(env.LocalStackHost, lsHost(srv))
				if op.withAuth {
					environ = environ.With(env.AuthToken, "test-token")
				}

				args := append([]string{"--config", em.writeConfig(t, "4566"), "--non-interactive"}, subArgs...)
				stdout, stderr, err := runLstk(t, ctx, dir, environ, args...)
				require.NoError(t, err, "lstk snapshot %s failed: %s", op.name, stderr)
				assert.Contains(t, stdout, op.successText)
				assert.Contains(t, stdout, experimentalWarningFragment)
				assert.Contains(t, stdout, em.name)
			})
		}
	}
}

// TestSnapshotSaveAWSNoExperimentalWarning guards the well-tested AWS path: it
// must not emit the experimental warning.
func TestSnapshotSaveAWSNoExperimentalWarning(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)
	srv := mockStateServer(t)
	dir := t.TempDir()

	stdout, stderr, err := runLstk(t, ctx, dir,
		env.Environ(testEnvWithHome(t.TempDir(), "")).With(env.LocalStackHost, lsHost(srv)),
		"--non-interactive", "snapshot", "save",
	)
	require.NoError(t, err, "lstk snapshot save failed: %s", stderr)
	assert.Contains(t, stdout, "Snapshot saved")
	assert.NotContains(t, stdout, experimentalWarningFragment)
}
