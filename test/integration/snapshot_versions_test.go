package integration_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// threeVersionPod is a platform payload with a deliberately out-of-order
// versions array, so the newest-first ordering has to come from lstk rather than
// from the response.
const threeVersionPod = `{
	"pod_name": "my-baseline",
	"max_version": 3,
	"versions": [
		{"version": 2, "localstack_version": "2026.05", "services": ["s3"], "created_at": 1750000000, "size": 2048},
		{"version": 3, "localstack_version": "2026.06", "services": ["s3", "lambda"], "created_at": 1776263520,
		 "storage_size": 49597645, "description": "nightly baseline"},
		{"version": 1, "localstack_version": "2026.04", "created_at": 1740000000}
	]
}`

// mockNeverCalledPlatform fails the test if the platform API is contacted at all
// — used by the cases that must be rejected client-side.
func mockNeverCalledPlatform(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("platform must not be called; got %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSnapshotVersionsSuccessWithoutDocker(t *testing.T) {
	t.Parallel()

	var cap showCapture
	srv := mockCloudPodServer(t, "my-baseline", threeVersionPod, &cap)

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(),
		listEnv(t, srv, "test-token"),
		"--non-interactive", "snapshot", "versions", "pod:my-baseline",
	)
	require.NoError(t, err, "lstk snapshot versions failed: %s", stderr)

	called, path, _ := cap.get()
	require.True(t, called, "the single-pod endpoint should have been called")
	assert.Equal(t, "/v1/cloudpods/my-baseline", path)

	assert.Contains(t, stdout, "~ 3 versions")
	for _, header := range []string{"VERSION", "CREATED", "LOCALSTACK", "SERVICES"} {
		assert.Contains(t, stdout, header)
	}
	assert.NotContains(t, stdout, "DESCRIPTION", "the description column was dropped in favour of more service names")
	assert.Contains(t, stdout, "2026-04-15 14:32 UTC")
	assert.Contains(t, stdout, "s3, lambda")
	assert.NotContains(t, stdout, "nightly baseline", "the description is no longer rendered")

	// Newest first: version 3's row must precede version 1's.
	assert.Less(t, strings.Index(stdout, "2026.06"), strings.Index(stdout, "2026.05"),
		"versions should be listed newest first")
	assert.Less(t, strings.Index(stdout, "2026.05"), strings.Index(stdout, "2026.04"),
		"versions should be listed newest first")
}

// TestSnapshotVersionsFiltersDeletedVersions: the platform marks removed versions
// rather than dropping them from the array, so they must not be offered as
// something the user could load.
func TestSnapshotVersionsFiltersDeletedVersions(t *testing.T) {
	t.Parallel()

	body := `{"pod_name": "p", "max_version": 3, "versions": [
		{"version": 1, "localstack_version": "keep-one"},
		{"version": 2, "localstack_version": "gone-two", "deleted": true},
		{"version": 3, "localstack_version": "keep-three"}
	]}`
	srv := mockCloudPodServer(t, "p", body, nil)

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(),
		listEnv(t, srv, "test-token"),
		"--non-interactive", "snapshot", "versions", "pod:p",
	)
	require.NoError(t, err, "lstk snapshot versions failed: %s", stderr)

	assert.Contains(t, stdout, "~ 2 versions", "the deleted version must not be counted")
	assert.Contains(t, stdout, "keep-one")
	assert.Contains(t, stdout, "keep-three")
	assert.NotContains(t, stdout, "gone-two")
}

func TestSnapshotVersionsSingleVersionUsesSingularNoun(t *testing.T) {
	t.Parallel()

	body := `{"pod_name": "solo", "max_version": 1, "versions": [{"version": 1}]}`
	srv := mockCloudPodServer(t, "solo", body, nil)

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(),
		listEnv(t, srv, "test-token"),
		"--non-interactive", "snapshot", "versions", "pod:solo",
	)
	require.NoError(t, err, "lstk snapshot versions failed: %s", stderr)
	assert.Contains(t, stdout, "~ 1 version\n")
}

func TestSnapshotVersionsEmptyHistory(t *testing.T) {
	t.Parallel()

	srv := mockCloudPodServer(t, "blank", `{"pod_name": "blank", "max_version": 0, "versions": []}`, nil)

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(),
		listEnv(t, srv, "test-token"),
		"--non-interactive", "snapshot", "versions", "pod:blank",
	)
	require.NoError(t, err, "lstk snapshot versions failed: %s", stderr)
	assert.Contains(t, stdout, "No versions found for 'pod:blank'")
	assert.NotContains(t, stdout, "VERSION", "no table should be rendered")
}

// TestSnapshotVersionsNoDockerRequired proves the command reads the platform API
// directly: with DOCKER_HOST pointed at a nonexistent socket it must still work,
// same as snapshot show and bare snapshot list.
func TestSnapshotVersionsNoDockerRequired(t *testing.T) {
	t.Parallel()

	srv := mockCloudPodServer(t, "my-baseline", threeVersionPod, nil)
	e := append(listEnv(t, srv, "test-token"), unreachableDockerHost)

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e,
		"--non-interactive", "snapshot", "versions", "pod:my-baseline",
	)
	require.NoError(t, err, "lstk snapshot versions failed: %s", stderr)
	assert.Contains(t, stdout, "~ 3 versions")
}

func TestSnapshotVersionsRejectsLocalPath(t *testing.T) {
	t.Parallel()

	srv := mockNeverCalledPlatform(t)

	_, stderr, err := runLstk(t, testContext(t), t.TempDir(),
		listEnv(t, srv, "test-token"),
		"--non-interactive", "snapshot", "versions", "./my-snapshot",
	)
	requireExitCode(t, 1, err)
	assert.Contains(t, strings.ToLower(stderr), "local")
	assert.Contains(t, stderr, "list versions of local snapshots")
}

func TestSnapshotVersionsRejectsS3Ref(t *testing.T) {
	t.Parallel()

	srv := mockNeverCalledPlatform(t)

	_, stderr, err := runLstk(t, testContext(t), t.TempDir(),
		listEnv(t, srv, "test-token"),
		"--non-interactive", "snapshot", "versions", "s3://bucket/prefix",
	)
	requireExitCode(t, 1, err)
	assert.Contains(t, stderr, "not yet supported")
}

// TestSnapshotShowPinnedVersion: show is read-only and the platform returns every
// field per-version, so "pod:<name>:<version>" reports that version's own
// metadata rather than the latest one's.
func TestSnapshotShowPinnedVersion(t *testing.T) {
	t.Parallel()

	body := `{
		"pod_name": "my-baseline",
		"max_version": 3,
		"versions": [
			{"version": 1, "localstack_version": "2026.04", "services": ["s3"], "size": 1024,
			 "description": "the first one", "created_at": 1740000000},
			{"version": 3, "localstack_version": "2026.06", "services": ["s3", "lambda"],
			 "storage_size": 49597645, "description": "the latest one", "created_at": 1776263520}
		]
	}`
	srv := mockCloudPodServer(t, "my-baseline", body, nil)

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(),
		listEnv(t, srv, "test-token"),
		"--non-interactive", "snapshot", "show", "pod:my-baseline:1",
	)
	require.NoError(t, err, "lstk snapshot show failed: %s", stderr)

	assert.Contains(t, stdout, "the first one")
	assert.Contains(t, stdout, "2026.04")
	assert.Contains(t, stdout, "1.0 KB")
	assert.NotContains(t, stdout, "the latest one", "the pinned version's metadata must win")
	assert.NotContains(t, stdout, "2026.06")
}

func TestSnapshotShowVersionNotFound(t *testing.T) {
	t.Parallel()

	body := `{"pod_name": "my-baseline", "max_version": 3, "versions": [{"version": 3}]}`
	srv := mockCloudPodServer(t, "my-baseline", body, nil)

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(),
		listEnv(t, srv, "test-token"),
		"--non-interactive", "snapshot", "show", "pod:my-baseline:99",
	)
	requireExitCode(t, 1, err)
	assert.Contains(t, stdout, "Version 99 of 'pod:my-baseline' not found")
	assert.Contains(t, stdout, "The highest available version is 3.")
	assert.Contains(t, stdout, "lstk snapshot versions pod:my-baseline")
}

// TestSnapshotVersionsRejectsVersionSuffix: this command lists every version, so
// pinning one is meaningless. Accepting and ignoring it would be misleading.
func TestSnapshotVersionsRejectsVersionSuffix(t *testing.T) {
	t.Parallel()

	srv := mockNeverCalledPlatform(t)

	_, stderr, err := runLstk(t, testContext(t), t.TempDir(),
		listEnv(t, srv, "test-token"),
		"--non-interactive", "snapshot", "versions", "pod:my-baseline:3",
	)
	requireExitCode(t, 1, err)
	assert.Contains(t, stderr, "drop the ':3'")
}

func TestSnapshotVersionsRejectsInvalidPodName(t *testing.T) {
	t.Parallel()

	srv := mockNeverCalledPlatform(t)

	_, stderr, err := runLstk(t, testContext(t), t.TempDir(),
		listEnv(t, srv, "test-token"),
		"--non-interactive", "snapshot", "versions", "pod:release.v1",
	)
	requireExitCode(t, 1, err)
	assert.Contains(t, stderr, "invalid pod name")
}

func TestSnapshotVersionsNotFound(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(),
		listEnv(t, srv, "test-token"),
		"--non-interactive", "snapshot", "versions", "pod:missing",
	)
	requireExitCode(t, 1, err)
	assert.Contains(t, stdout, "not found")
	assert.Contains(t, stdout, "lstk snapshot list")
}

func TestSnapshotVersionsRequiresAuthToken(t *testing.T) {
	t.Parallel()

	srv := mockNeverCalledPlatform(t)
	environ := env.Environ(testEnvWithHome(t.TempDir(), "")).
		With(env.APIEndpoint, srv.URL).
		Without(env.AuthToken)

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), environ,
		"--non-interactive", "snapshot", "versions", "pod:my-baseline",
	)
	requireExitCode(t, 1, err)
	assert.Contains(t, stdout, "Authentication required")
	assert.Contains(t, stdout, "lstk login")
}
