package integration_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/localstack/lstk/internal/must"
	"github.com/localstack/lstk/internal/snap"
	"github.com/localstack/lstk/test/integration/env"
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
	must.NoError(t, err, "lstk snapshot versions failed: %s", stderr)

	called, path, _ := cap.get()
	must.True(t, called, "the single-pod endpoint should have been called")
	must.Eq(t, "/v1/cloudpods/my-baseline", path)

	// The snapshot pins column set (no DESCRIPTION), newest-first ordering,
	// and that descriptions are no longer rendered.
	snap.Match(t, sanitizeOutput(stdout))
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
	must.NoError(t, err, "lstk snapshot versions failed: %s", stderr)

	snap.Match(t, sanitizeOutput(stdout))
}

func TestSnapshotVersionsSingleVersionUsesSingularNoun(t *testing.T) {
	t.Parallel()

	body := `{"pod_name": "solo", "max_version": 1, "versions": [{"version": 1}]}`
	srv := mockCloudPodServer(t, "solo", body, nil)

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(),
		listEnv(t, srv, "test-token"),
		"--non-interactive", "snapshot", "versions", "pod:solo",
	)
	must.NoError(t, err, "lstk snapshot versions failed: %s", stderr)
	must.Contains(t, stdout, "~ 1 version\n")
}

func TestSnapshotVersionsEmptyHistory(t *testing.T) {
	t.Parallel()

	srv := mockCloudPodServer(t, "blank", `{"pod_name": "blank", "max_version": 0, "versions": []}`, nil)

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(),
		listEnv(t, srv, "test-token"),
		"--non-interactive", "snapshot", "versions", "pod:blank",
	)
	must.NoError(t, err, "lstk snapshot versions failed: %s", stderr)
	must.Contains(t, stdout, "No versions found for 'pod:blank'")
	must.NotContains(t, stdout, "VERSION", "no table should be rendered")
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
	must.NoError(t, err, "lstk snapshot versions failed: %s", stderr)
	must.Contains(t, stdout, "~ 3 versions")
}

func TestSnapshotVersionsRejectsLocalPath(t *testing.T) {
	t.Parallel()

	srv := mockNeverCalledPlatform(t)

	_, stderr, err := runLstk(t, testContext(t), t.TempDir(),
		listEnv(t, srv, "test-token"),
		"--non-interactive", "snapshot", "versions", "./my-snapshot",
	)
	requireExitCode(t, 1, err)
	must.Contains(t, strings.ToLower(stderr), "local")
	must.Contains(t, stderr, "list versions of local snapshots")
}

// TestSnapshotVersionsRejectsS3Ref: S3 remotes are fully supported by
// save/load/list, so the generic "coming soon" wording used for unimplemented
// remotes would wrongly imply versions-on-S3 is merely pending. Only version
// history is pod-only, and the message has to say so.
func TestSnapshotVersionsRejectsS3Ref(t *testing.T) {
	t.Parallel()

	srv := mockNeverCalledPlatform(t)

	_, stderr, err := runLstk(t, testContext(t), t.TempDir(),
		listEnv(t, srv, "test-token"),
		"--non-interactive", "snapshot", "versions", "s3://bucket/prefix",
	)
	requireExitCode(t, 1, err)
	must.Contains(t, stderr, "only supported for Cloud Pods")
	must.Contains(t, stderr, "S3 remotes")
	must.NotContains(t, stderr, "coming soon")
}

// TestSnapshotVersionsOrasKeepsComingSoon: oras:// is unimplemented for every
// command, so the generic wording is correct there — the S3 fix must not have
// changed it.
func TestSnapshotVersionsOrasKeepsComingSoon(t *testing.T) {
	t.Parallel()

	srv := mockNeverCalledPlatform(t)

	_, stderr, err := runLstk(t, testContext(t), t.TempDir(),
		listEnv(t, srv, "test-token"),
		"--non-interactive", "snapshot", "versions", "oras://registry/image",
	)
	requireExitCode(t, 1, err)
	must.Contains(t, stderr, "coming soon")
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
	must.NoError(t, err, "lstk snapshot show failed: %s", stderr)

	// Snapshot of version 1's card: the pinned version's metadata must win
	// over the latest version's.
	snap.Match(t, sanitizeOutput(stdout))
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
	must.Contains(t, stdout, "Version 99 of 'pod:my-baseline' not found")
	must.Contains(t, stdout, "The highest available version is 3.")
	must.Contains(t, stdout, "lstk snapshot versions pod:my-baseline")
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
	must.Contains(t, stderr, "drop the ':3'")
}

func TestSnapshotVersionsRejectsInvalidPodName(t *testing.T) {
	t.Parallel()

	srv := mockNeverCalledPlatform(t)

	_, stderr, err := runLstk(t, testContext(t), t.TempDir(),
		listEnv(t, srv, "test-token"),
		"--non-interactive", "snapshot", "versions", "pod:release.v1",
	)
	requireExitCode(t, 1, err)
	must.Contains(t, stderr, "invalid pod name")
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
	must.Contains(t, stdout, "not found")
	must.Contains(t, stdout, "lstk snapshot list")
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
	must.Contains(t, stdout, "Authentication required")
	must.Contains(t, stdout, "lstk login")
}
