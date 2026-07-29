package integration_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// unreachableDockerHost points DOCKER_HOST at a socket that can't possibly
// exist, so any code path that actually tries to talk to Docker fails
// immediately and loudly — a clean way to prove a command took the
// --endpoint-url path without touching Docker at all, without needing a real
// Docker daemon to be unavailable in the test environment.
const unreachableDockerHost = "DOCKER_HOST=unix:///nonexistent/docker.sock"

// awsHealthServer starts an httptest.Server that answers /_localstack/health
// like a real AWS-flavored LocalStack emulator (see the community image
// payload recorded in design.md's research for add-endpoint-url-flag).
func awsHealthServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_localstack/health" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"version":  "3.0.2",
			"edition":  "community",
			"services": map[string]string{"s3": "available", "sqs": "available"},
		})
	}))
}

func writeFakeAWSEcho(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake aws script not supported on Windows")
	}
	dir := t.TempDir()
	script := `#!/bin/sh
echo "ENDPOINT:$2"
shift 2
echo "ARGS:$@"
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "aws"), []byte(script), 0755))
	return dir
}

// TestAWSCommandEndpointURLNoDockerRequired proves --endpoint-url (placed
// before the subcommand) lets `lstk aws` skip Docker discovery entirely: with
// DOCKER_HOST pointed at a nonexistent socket, the command must still succeed
// by talking directly to the mock LocalStack server.
func TestAWSCommandEndpointURLNoDockerRequired(t *testing.T) {
	t.Parallel()
	srv := awsHealthServer(t)
	defer srv.Close()

	fakeDir := writeFakeAWSEcho(t)
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())
	e = append(e, unreachableDockerHost)

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "--endpoint-url", srv.URL, "aws", "s3", "ls")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Contains(t, stdout, "ENDPOINT:"+srv.URL)
	assert.Contains(t, stdout, "ARGS:s3 ls")
}

// TestAWSCommandEndpointURLAfterSubcommandPassesThrough proves the aws-CLI
// collision fix: --endpoint-url placed AFTER "aws" is the AWS CLI's own
// native flag and must reach the wrapped binary untouched rather than being
// intercepted by lstk. A pre-command --endpoint-url (a distinct mock server)
// is also given, so lstk's own resolution — and thus its Docker bypass —
// succeeds independently of whatever's running locally; the post-command
// occurrence is asserted to show up verbatim in the args forwarded to the
// wrapped binary, not consumed by lstk.
func TestAWSCommandEndpointURLAfterSubcommandPassesThrough(t *testing.T) {
	t.Parallel()
	srv := awsHealthServer(t)
	defer srv.Close()

	fakeDir := writeFakeAWSEcho(t)
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())
	e = append(e, unreachableDockerHost)

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e,
		"--endpoint-url", srv.URL, "aws", "--endpoint-url", "http://127.0.0.1:9", "s3", "ls")
	require.NoError(t, err, "stderr: %s", stderr)
	// lstk's own resolution used the pre-command mock — proven by succeeding
	// at all with DOCKER_HOST broken.
	assert.Contains(t, stdout, "ENDPOINT:"+srv.URL)
	// The user's own post-command --endpoint-url reached the wrapped aws
	// binary untouched, appearing in its forwarded args.
	assert.Contains(t, stdout, "ARGS:--endpoint-url http://127.0.0.1:9 s3 ls")
}

// TestLogsRejectsExplicitEndpointURL proves logs (a Docker-lifecycle command
// with no remote equivalent) rejects an explicit --endpoint-url outright,
// before ever touching Docker.
func TestLogsRejectsExplicitEndpointURL(t *testing.T) {
	t.Parallel()
	e := env.With(env.DisableEvents, "1").With(env.Home, t.TempDir())
	e = append(e, unreachableDockerHost)

	// The rejection is rendered as an ErrorEvent through the plain sink
	// (stdout), with a silent error returned so the top-level handler doesn't
	// print a second copy to stderr.
	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), e, "logs", "--endpoint-url", "http://localhost:4566")
	require.Error(t, err)
	assert.Contains(t, stdout, "logs")
	assert.Contains(t, stdout, "does not support --endpoint-url")
}

// TestVolumePathRejectsExplicitEndpointURL mirrors the logs case for the
// other Docker-lifecycle/filesystem commands.
func TestVolumePathRejectsExplicitEndpointURL(t *testing.T) {
	t.Parallel()
	e := env.With(env.DisableEvents, "1").With(env.Home, t.TempDir())

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), e, "volume", "path", "--endpoint-url", "http://localhost:4566")
	require.Error(t, err)
	assert.Contains(t, stdout, "does not support --endpoint-url")
}

// TestSnapshotShowIgnoresEndpointURL proves `snapshot show` silently ignores
// --endpoint-url (it only ever talks to the LocalStack platform API, which is
// account-scoped rather than emulator-scoped) rather than rejecting it: with
// the platform API pointed at an unreachable address, the command still
// attempts — and fails on — that platform call, not on flag validation.
func TestSnapshotShowIgnoresEndpointURL(t *testing.T) {
	t.Parallel()
	e := env.With(env.DisableEvents, "1").With(env.Home, t.TempDir()).With(env.APIEndpoint, "http://127.0.0.1:1")

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "snapshot", "show", "pod:my-baseline", "--endpoint-url", "http://localhost:4566")
	require.Error(t, err)
	assert.NotContains(t, stdout, "does not support --endpoint-url")
	assert.NotContains(t, stderr, "does not support --endpoint-url")
}

// TestSnapshotListBareIgnoresEndpointURL mirrors the show case for `snapshot
// list` with no s3:// argument.
func TestSnapshotListBareIgnoresEndpointURL(t *testing.T) {
	t.Parallel()
	e := env.With(env.DisableEvents, "1").With(env.Home, t.TempDir()).With(env.APIEndpoint, "http://127.0.0.1:1")

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "snapshot", "list", "--endpoint-url", "http://localhost:4566")
	require.Error(t, err)
	assert.NotContains(t, stdout, "does not support --endpoint-url")
	assert.NotContains(t, stderr, "does not support --endpoint-url")
}

// TestStatusEndpointURLRendersReducedOutput proves `status` skips Docker
// entirely for an externally-managed endpoint and renders reachability +
// detected type + version, without container/uptime fields.
func TestStatusEndpointURLRendersReducedOutput(t *testing.T) {
	t.Parallel()
	srv := awsHealthServer(t)
	defer srv.Close()

	e := env.With(env.DisableEvents, "1").With(env.Home, t.TempDir())
	e = append(e, unreachableDockerHost)

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "--endpoint-url", srv.URL, "status")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Contains(t, stdout, "running")
	assert.Contains(t, stdout, srv.URL[len("http://"):])
	assert.Contains(t, stdout, "3.0.2")
	assert.NotContains(t, stdout, "Container:")
	assert.NotContains(t, stdout, "Uptime:")
}

// TestStatusUnreachableEndpointURLFailsClosed proves an endpoint that
// doesn't look like a genuine LocalStack instance is rejected with a clear
// error rather than silently proceeding.
func TestStatusUnreachableEndpointURLFailsClosed(t *testing.T) {
	t.Parallel()
	notLocalStack := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer notLocalStack.Close()

	e := env.With(env.DisableEvents, "1").With(env.Home, t.TempDir())
	e = append(e, unreachableDockerHost)

	_, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "--endpoint-url", notLocalStack.URL, "status")
	require.Error(t, err)
	assert.Contains(t, stderr, "could not reach")
	assert.NotContains(t, stderr, "lstk start")
}

// TestCDKAWSEndpointURLBypassesDockerCheck proves the BREAKING change:
// AWS_ENDPOINT_URL alone (no --endpoint-url/LSTK_ENDPOINT_URL) now skips
// Docker discovery entirely for an AWS-contacting cdk subcommand, where
// previously it only relabeled the value while Docker discovery still ran.
func TestCDKAWSEndpointURLBypassesDockerCheck(t *testing.T) {
	t.Parallel()
	srv := awsHealthServer(t)
	defer srv.Close()

	fakeDir := writeFakeCDK(t, "2.177.0")
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir()).
		With("AWS_ENDPOINT_URL", srv.URL)
	e = append(e, unreachableDockerHost)

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "cdk", "deploy", "MyStack")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Contains(t, stdout, fmt.Sprintf("ENV_AWS_ENDPOINT_URL=%s", srv.URL))
}

// TestCDKAWSEndpointURLWrongTypeFails proves the type-detection gate: an
// endpoint detected as non-AWS is rejected with an AWS-specific error for an
// AWS-only proxy command like cdk.
func TestCDKAWSEndpointURLWrongTypeFails(t *testing.T) {
	t.Parallel()
	azureLike := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/_localstack/health":
			_ = json.NewEncoder(w).Encode(map[string]any{"services": map[string]string{}})
		case "/_localstack/info":
			_ = json.NewEncoder(w).Encode(map[string]any{"version": "3.0.2"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer azureLike.Close()

	fakeDir := writeFakeCDK(t, "2.177.0")
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())
	e = append(e, unreachableDockerHost)

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), e, "--endpoint-url", azureLike.URL, "cdk", "deploy", "MyStack")
	require.Error(t, err)
	assert.Contains(t, stdout, "requires the AWS emulator")
	assert.Contains(t, stdout, "Azure")
}
