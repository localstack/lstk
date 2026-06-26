package integration_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
		env.Environ(testEnvWithHome(t.TempDir(), "")).With(env.LocalStackHost, lsHost(srv)),
		"--non-interactive", "snapshot", "save",
	)
	require.NoError(t, err, "lstk snapshot save failed: %s", stderr)
	assert.Contains(t, stdout, "Snapshot saved")

	entries, readErr := os.ReadDir(dir)
	require.NoError(t, readErr)
	var found bool
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "snapshot-") && strings.HasSuffix(e.Name(), ".snapshot") {
			found = true
			break
		}
	}
	assert.True(t, found, "default snapshot file (snapshot-*.snapshot) should exist in %s", dir)
}

func TestSnapshotSaveCustomPath(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)
	srv := mockStateServer(t)
	dir := t.TempDir()
	outPath := filepath.Join(dir, "my-snap.snapshot")

	stdout, stderr, err := runLstk(t, ctx, dir,
		env.Environ(testEnvWithHome(t.TempDir(), "")).With(env.LocalStackHost, lsHost(srv)),
		"--non-interactive", "snapshot", "save", outPath,
	)
	require.NoError(t, err, "lstk snapshot save failed: %s", stderr)
	assert.Contains(t, stdout, "Snapshot saved")
	assert.Contains(t, stdout, "./my-snap.snapshot")

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
		env.Environ(testEnvWithHome(t.TempDir(), "")).With(env.LocalStackHost, lsHost(srv)),
		"--non-interactive", "snapshot", "save", "./my-state",
	)
	require.NoError(t, err, "lstk snapshot save failed: %s", stderr)
	assert.Contains(t, stdout, "Snapshot saved")

	_, statErr := os.Stat(filepath.Join(dir, "my-state.snapshot"))
	assert.NoError(t, statErr, "relative output file should exist")
}

// TestSnapshotSaveForcesSnapshotExtension verifies that a user-supplied extension
// is replaced with .snapshot rather than honored verbatim.
func TestSnapshotSaveForcesSnapshotExtension(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)
	srv := mockStateServer(t)
	dir := t.TempDir()

	stdout, stderr, err := runLstk(t, ctx, dir,
		env.Environ(testEnvWithHome(t.TempDir(), "")).With(env.LocalStackHost, lsHost(srv)),
		"--non-interactive", "snapshot", "save", "./x.zip",
	)
	require.NoError(t, err, "lstk snapshot save failed: %s", stderr)
	assert.Contains(t, stdout, "./x.snapshot")

	_, statErr := os.Stat(filepath.Join(dir, "x.snapshot"))
	assert.NoError(t, statErr, "extension should be forced to .snapshot")
	_, zipErr := os.Stat(filepath.Join(dir, "x.zip"))
	assert.True(t, os.IsNotExist(zipErr), "the user-supplied .zip path should not be created")
}

func TestSnapshotSaveOverwritesExistingFile(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)
	srv := mockStateServer(t)
	dir := t.TempDir()
	outPath := filepath.Join(dir, "snap.snapshot")
	require.NoError(t, os.WriteFile(outPath, []byte("OLD"), 0600))

	_, stderr, err := runLstk(t, ctx, dir,
		env.Environ(testEnvWithHome(t.TempDir(), "")).With(env.LocalStackHost, lsHost(srv)),
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
		"oras://registry/my-snap",
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

// TestSnapshotSaveS3MissingCredentials and the credential/URL validation tests run
// before any Docker interaction, so they need no running emulator.
func TestSnapshotSaveS3MissingCredentials(t *testing.T) {
	t.Parallel()
	ctx := testContext(t)

	_, stderr, err := runLstk(t, ctx, t.TempDir(),
		env.Environ(testEnvWithHome(t.TempDir(), "")).Without(env.AWSAccessKeyID, env.AWSSecretAccessKey),
		"--non-interactive", "snapshot", "save", "my-pod", "s3://my-bucket/prefix",
	)
	requireExitCode(t, 1, err)
	assert.Contains(t, stderr, "AWS credentials required")
}

func TestSnapshotSaveS3CredentialsInURLRejected(t *testing.T) {
	t.Parallel()
	ctx := testContext(t)

	_, stderr, err := runLstk(t, ctx, t.TempDir(),
		env.Environ(testEnvWithHome(t.TempDir(), "")).
			With(env.AWSAccessKeyID, "AKIA").
			With(env.AWSSecretAccessKey, "secret"),
		"--non-interactive", "snapshot", "save", "my-pod", "s3://my-bucket/prefix?access_key_id=AKIA&secret_access_key=secret",
	)
	requireExitCode(t, 1, err)
	assert.Contains(t, stderr, "do not put credentials")
}

// mockPodS3Server handles the remote registration plus pod save against an S3
// remote, capturing the registered remote URL and the save request body so the
// test can assert the wire contract (placeholders in the URL, secrets only in the
// ephemeral params).
func mockPodS3Server(t *testing.T) (*httptest.Server, func() (remoteURL string, saveBody []byte)) {
	t.Helper()
	var (
		mu        sync.Mutex
		remoteURL string
		saveBody  []byte
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/_localstack/pods/remotes/") && r.Method == http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			var parsed struct {
				RemoteURL string `json:"remote_url"`
			}
			_ = json.Unmarshal(body, &parsed)
			mu.Lock()
			remoteURL = parsed.RemoteURL
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
		case strings.HasPrefix(r.URL.Path, "/_localstack/pods/") && r.Method == http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			mu.Lock()
			saveBody = body
			mu.Unlock()
			w.Header().Set("Content-Type", "application/x-ndjson")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"event":"completion","status":"ok","operation":"save","info":{"name":"my-pod","version":1,"services":["s3"],"size":2048}}` + "\n"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, func() (string, []byte) {
		mu.Lock()
		defer mu.Unlock()
		return remoteURL, saveBody
	}
}

func TestSnapshotSaveS3Success(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)
	srv, captured := mockPodS3Server(t)

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(),
		env.Environ(testEnvWithHome(t.TempDir(), "")).
			With(env.LocalStackHost, lsHost(srv)).
			With(env.AWSAccessKeyID, "AKIAEXAMPLE").
			With(env.AWSSecretAccessKey, "topsecret"),
		"--non-interactive", "snapshot", "save", "my-pod", "s3://my-bucket/prefix",
	)
	require.NoError(t, err, "lstk snapshot save to s3 failed: %s", stderr)
	assert.Contains(t, stdout, "Snapshot saved to s3://my-bucket/prefix")
	assert.Contains(t, stdout, "my-pod")

	remoteURL, saveBody := captured()
	// The registered URL carries placeholders, never the secret values.
	assert.Contains(t, remoteURL, "{access_key_id}")
	assert.NotContains(t, remoteURL, "topsecret")
	assert.NotContains(t, remoteURL, "AKIAEXAMPLE")
	// The secrets travel only in the ephemeral save params.
	assert.Contains(t, string(saveBody), "topsecret")
	assert.Contains(t, string(saveBody), "remote_params")
}

// mockPodSaveServer returns a test server that handles POST /_localstack/pods/{name}
// and responds with a streaming completion event. respondOK controls whether the
// completion event reports success or a server-side error.
func mockPodSaveServer(t *testing.T, respondOK bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/_localstack/pods/") && r.Method == http.MethodPost {
			w.Header().Set("Content-Type", "application/x-ndjson")
			w.WriteHeader(http.StatusOK)
			if respondOK {
				_, _ = w.Write([]byte(`{"event":"completion","status":"ok","operation":"save","info":{"name":"my-baseline","version":1,"remote":"platform","services":["dynamodb","s3"],"size":1048576}}` + "\n"))
			} else {
				_, _ = w.Write([]byte(`{"event":"completion","status":"error","message":"platform error"}` + "\n"))
			}
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSnapshotSavePodSuccess(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)
	srv := mockPodSaveServer(t, true)

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(),
		env.Environ(testEnvWithHome(t.TempDir(), "")).
			With(env.LocalStackHost, lsHost(srv)).
			With(env.AuthToken, "test-token"),
		"--non-interactive", "snapshot", "save", "pod:my-baseline",
	)
	require.NoError(t, err, "lstk snapshot save pod:my-baseline failed: %s", stderr)
	assert.Contains(t, stdout, "Snapshot saved")
	assert.Contains(t, stdout, "my-baseline")
	assert.Contains(t, stdout, "Version: 1")
	assert.Contains(t, stdout, "dynamodb, s3")
}

func TestSnapshotSavePodServerError(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)
	srv := mockPodSaveServer(t, false)

	_, stderr, err := runLstk(t, ctx, t.TempDir(),
		env.Environ(testEnvWithHome(t.TempDir(), "")).
			With(env.LocalStackHost, lsHost(srv)).
			With(env.AuthToken, "test-token"),
		"--non-interactive", "snapshot", "save", "pod:my-baseline",
	)
	requireExitCode(t, 1, err)
	assert.Contains(t, stderr, "platform error")
}

func TestSnapshotSavePodNoAuthToken(t *testing.T) {
	t.Parallel()
	ctx := testContext(t)

	_, stderr, err := runLstk(t, ctx, t.TempDir(),
		env.Environ(testEnvWithHome(t.TempDir(), "")).Without(env.AuthToken),
		"--non-interactive", "snapshot", "save", "pod:my-baseline",
	)
	requireExitCode(t, 1, err)
	assert.Contains(t, stderr, "authentication")
}

func TestSnapshotSavePodInvalidName(t *testing.T) {
	t.Parallel()
	for _, dest := range []string{
		"pod:",
		"pod:-invalid",
		"pod:_invalid",
		"pod:my pod",
	} {
		t.Run(dest, func(t *testing.T) {
			t.Parallel()
			ctx := testContext(t)

			_, stderr, err := runLstk(t, ctx, t.TempDir(), testEnvWithHome(t.TempDir(), ""), "--non-interactive", "snapshot", "save", dest)
			requireExitCode(t, 1, err)
			assert.Contains(t, stderr, "invalid pod name")
		})
	}
}

func TestSnapshotSaveEmulatorNotRunning(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	tests := []struct {
		name      string
		args      []string
		authToken string
	}{
		{
			name: "local destination",
			args: []string{"--non-interactive", "snapshot", "save"},
		},
		{
			name:      "pod destination",
			args:      []string{"--non-interactive", "snapshot", "save", "pod:my-baseline"},
			authToken: "test-token",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Intentionally no startTestContainer: the emulator is not running.
			ctx := testContext(t)
			e := env.Environ(testEnvWithHome(t.TempDir(), ""))
			if tc.authToken != "" {
				e = e.With(env.AuthToken, tc.authToken)
			}
			stdout, _, err := runLstk(t, ctx, t.TempDir(), e, tc.args...)
			requireExitCode(t, 1, err)
			assert.Contains(t, stdout, "not running")
		})
	}
}

func TestSnapshotSaveInvalidParentDir(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)
	srv := mockStateServer(t)

	_, stderr, err := runLstk(t, ctx, t.TempDir(),
		env.Environ(testEnvWithHome(t.TempDir(), "")).With(env.LocalStackHost, lsHost(srv)),
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
		env.Environ(testEnvWithHome(t.TempDir(), "")).With(env.LocalStackHost, lsHost(srv)).With(env.AnalyticsEndpoint, analyticsSrv.URL),
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
		env.Environ(testEnvWithHome(t.TempDir(), "")).With(env.AnalyticsEndpoint, analyticsSrv.URL),
		"--non-interactive", "snapshot", "save",
	)
	requireExitCode(t, 1, err)
	assertCommandTelemetry(t, events, "snapshot save", 1)
}

func TestSaveAliasMatchesSnapshotSave(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)
	srv := mockStateServer(t)
	dir := t.TempDir()
	outPath := filepath.Join(dir, "alias.snapshot")

	analyticsSrv, events := mockAnalyticsServer(t)
	stdout, stderr, err := runLstk(t, ctx, dir,
		env.Environ(testEnvWithHome(t.TempDir(), "")).With(env.LocalStackHost, lsHost(srv)).With(env.AnalyticsEndpoint, analyticsSrv.URL),
		"--non-interactive", "save", outPath,
	)
	require.NoError(t, err, "lstk save failed: %s", stderr)
	assert.Contains(t, stdout, "Snapshot saved")

	data, err := os.ReadFile(outPath)
	require.NoError(t, err, "output file should exist")
	assert.True(t, len(data) > 0, "output file should be non-empty")

	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	require.NoError(t, err, "output file should be a valid ZIP")
	assert.NotEmpty(t, r.File)

	// Alias must emit telemetry under the canonical name so usage isn't
	// split across "save" and "snapshot save" labels.
	assertCommandTelemetry(t, events, "snapshot save", 0)
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
		env.Environ(testEnvWithHome(t.TempDir(), "")).With(env.LocalStackHost, lsHost(srv)),
		"snapshot", "save", filepath.Join(dir, "snap"),
	)
	require.NoError(t, err, "interactive lstk snapshot save failed")
	assert.Contains(t, out, "Snapshot saved")
}
