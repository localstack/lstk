package integration_test

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockStateServer returns a test server that serves a minimal ZIP at /_localstack/pods/state.
func mockStateServer(t *testing.T) *httptest.Server {
	t.Helper()
	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	f, err := zw.Create("state.json")
	require.NoError(t, err)
	_, err = f.Write([]byte(`{"services":{}}`))
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	zipData := zipBuf.Bytes()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_localstack/pods/state" {
			w.Header().Set("Content-Type", "application/zip")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(zipData)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func lsHost(srv *httptest.Server) string {
	return strings.TrimPrefix(srv.URL, "http://")
}

func TestSnapshotSaveDefaultDestination(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)
	srv := mockStateServer(t)
	dir := t.TempDir()

	stdout, stderr, err := runLstk(t, ctx, dir,
		env.With(env.LocalStackHost, lsHost(srv)),
		"--non-interactive", "snapshot", "save",
	)
	require.NoError(t, err, "lstk snapshot save failed: %s", stderr)
	assert.Contains(t, stdout, "Snapshot saved")

	entries, readErr := os.ReadDir(dir)
	require.NoError(t, readErr)
	var found bool
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "snapshot-") {
			found = true
			break
		}
	}
	assert.True(t, found, "default snapshot file (snapshot-*) should exist in %s", dir)
}

func TestSnapshotSaveCustomPath(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)
	srv := mockStateServer(t)
	dir := t.TempDir()
	outPath := filepath.Join(dir, "my-snap.zip")

	stdout, stderr, err := runLstk(t, ctx, dir,
		env.With(env.LocalStackHost, lsHost(srv)),
		"--non-interactive", "snapshot", "save", outPath,
	)
	require.NoError(t, err, "lstk snapshot save failed: %s", stderr)
	assert.Contains(t, stdout, "Snapshot saved")
	assert.Contains(t, stdout, "./my-snap.zip")

	data, err := os.ReadFile(outPath)
	require.NoError(t, err, "output file should exist")
	assert.True(t, len(data) > 0, "output file should be non-empty")

	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	require.NoError(t, err, "output file should be a valid ZIP")
	assert.NotEmpty(t, r.File)
}

func TestSnapshotSaveRelativePath(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)
	srv := mockStateServer(t)
	dir := t.TempDir()

	stdout, stderr, err := runLstk(t, ctx, dir,
		env.With(env.LocalStackHost, lsHost(srv)),
		"--non-interactive", "snapshot", "save", "./my-state",
	)
	require.NoError(t, err, "lstk snapshot save failed: %s", stderr)
	assert.Contains(t, stdout, "Snapshot saved")

	_, statErr := os.Stat(filepath.Join(dir, "my-state.zip"))
	assert.NoError(t, statErr, "relative output file should exist")
}

func TestSnapshotSaveOverwritesExistingFile(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)
	srv := mockStateServer(t)
	dir := t.TempDir()
	outPath := filepath.Join(dir, "snap.zip")
	require.NoError(t, os.WriteFile(outPath, []byte("OLD"), 0600))

	_, stderr, err := runLstk(t, ctx, dir,
		env.With(env.LocalStackHost, lsHost(srv)),
		"--non-interactive", "snapshot", "save", outPath,
	)
	require.NoError(t, err, "lstk snapshot save should overwrite: %s", stderr)

	data, err := os.ReadFile(outPath)
	require.NoError(t, err)
	assert.NotEqual(t, "OLD", string(data), "file should have been overwritten")
}

func TestSnapshotSaveRemoteRejected(t *testing.T) {
	t.Parallel()
	for _, dest := range []string{
		"s3://my-bucket/my-snap",
		"oras://registry/my-snap",
		"cloud://my-pod",
	} {
		t.Run(dest, func(t *testing.T) {
			t.Parallel()
			ctx := testContext(t)

			_, stderr, err := runLstk(t, ctx, t.TempDir(), testEnvWithHome(t.TempDir(), ""), "--non-interactive", "snapshot", "save", dest)
			requireExitCode(t, 1, err)
			assert.Contains(t, stderr, "not yet supported")
		})
	}
}

func TestSnapshotSaveLocalStackNotRunning(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	// Intentionally no startTestContainer: the emulator is not running.

	stdout, _, err := runLstk(t, ctx, t.TempDir(), testEnvWithHome(t.TempDir(), ""),
		"--non-interactive", "snapshot", "save",
	)
	requireExitCode(t, 1, err)
	assert.Contains(t, stdout, "not running")
}

func TestSnapshotSaveInvalidParentDir(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)
	srv := mockStateServer(t)

	_, stderr, err := runLstk(t, ctx, t.TempDir(),
		env.With(env.LocalStackHost, lsHost(srv)),
		"--non-interactive", "snapshot", "save", "/no/such/dir/state",
	)
	requireExitCode(t, 1, err)
	assert.NotEmpty(t, stderr)
}

func TestSnapshotSaveTelemetryEmitted(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)
	srv := mockStateServer(t)

	analyticsSrv, events := mockAnalyticsServer(t)
	_, stderr, err := runLstk(t, ctx, t.TempDir(),
		env.With(env.LocalStackHost, lsHost(srv)).With(env.AnalyticsEndpoint, analyticsSrv.URL),
		"--non-interactive", "snapshot", "save",
	)
	require.NoError(t, err, "lstk snapshot save failed: %s", stderr)
	assertCommandTelemetry(t, events, "snapshot save", 0)
}

func TestSnapshotSaveTelemetryOnFailure(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	// No container running → "LocalStack is not running" failure.

	analyticsSrv, events := mockAnalyticsServer(t)
	_, _, err := runLstk(t, ctx, t.TempDir(),
		env.With(env.AnalyticsEndpoint, analyticsSrv.URL),
		"--non-interactive", "snapshot", "save",
	)
	requireExitCode(t, 1, err)
	assertCommandTelemetry(t, events, "snapshot save", 1)
}

func TestSnapshotSaveInteractive(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)
	srv := mockStateServer(t)
	dir := t.TempDir()

	out, err := runLstkInPTY(t, ctx,
		env.With(env.LocalStackHost, lsHost(srv)),
		"snapshot", "save", filepath.Join(dir, "snap"),
	)
	require.NoError(t, err, "interactive lstk snapshot save failed")
	assert.Contains(t, out, "Snapshot saved")
}
