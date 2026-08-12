package integration_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/localstack/lstk/internal/snap"
	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// unreachableDockerHost points DOCKER_HOST at a closed TCP port (valid on
// every OS, unlike a unix socket path), so any code path that actually tries
// to talk to Docker fails immediately and loudly — a clean way to prove a
// command took the --endpoint-url path without touching Docker at all,
// without needing a real Docker daemon to be unavailable in the test
// environment.
const unreachableDockerHost = "DOCKER_HOST=tcp://localhost:1"

// awsHealthHandler answers /_localstack/health like a real AWS-flavored
// LocalStack emulator (see the community image payload recorded in design.md's
// research for add-endpoint-url-flag). It also answers /_localstack/resources
// with an empty listing, since a real emulator serves that endpoint and
// `status` treats a failure to fetch resources as fatal — on either the Docker
// or the --endpoint-url path. Shared by awsHealthServer and awsHealthTLSServer
// so the http and https mocks cannot drift apart.
func awsHealthHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/_localstack/health":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"version":  "3.0.2",
				"edition":  "community",
				"services": map[string]string{"s3": "available", "sqs": "available"},
			})
		case "/_localstack/resources":
			w.Header().Set("Content-Type", "application/x-ndjson")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

// awsHealthServer starts an httptest.Server serving awsHealthHandler.
func awsHealthServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(awsHealthHandler())
}

func writeFakeAWSEcho(t *testing.T) string {
	t.Helper()
	return writeFakeTool(t, "aws", fakeToolConfig{
		Shift:  2,
		Stdout: []string{"ENDPOINT:{arg2}", "ARGS:{args}"},
	})
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
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(t.TempDir())
	e = append(e, unreachableDockerHost)

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "--endpoint-url", srv.URL, "aws", "s3", "ls")
	require.NoError(t, err, "stderr: %s", stderr)
	snap.Match(t, sanitizeOutput(stdout))
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
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(t.TempDir())
	e = append(e, unreachableDockerHost)

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e,
		"--endpoint-url", srv.URL, "aws", "--endpoint-url", "http://127.0.0.1:9", "s3", "ls")
	require.NoError(t, err, "stderr: %s", stderr)
	// The snapshot pins both facts: lstk's own resolution used the pre-command
	// mock (ENDPOINT line — proven by succeeding at all with DOCKER_HOST
	// broken), and the user's own post-command --endpoint-url reached the
	// wrapped aws binary untouched, appearing in its forwarded args.
	snap.Match(t, sanitizeOutput(stdout))
}

// TestLogsRejectsExplicitEndpointURL proves logs (a Docker-lifecycle command
// with no remote equivalent) rejects an explicit --endpoint-url outright,
// before ever touching Docker.
func TestLogsRejectsExplicitEndpointURL(t *testing.T) {
	t.Parallel()
	e := env.With(env.DisableEvents, "1").WithHome(t.TempDir())
	e = append(e, unreachableDockerHost)

	// The rejection is rendered as an ErrorEvent through the plain sink
	// (stdout), with a silent error returned so the top-level handler doesn't
	// print a second copy to stderr.
	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), e, "logs", "--endpoint-url", "http://localhost:4566")
	require.Error(t, err)
	snap.Match(t, sanitizeOutput(stdout))
}

// TestVolumePathRejectsExplicitEndpointURL mirrors the logs case for the
// other Docker-lifecycle/filesystem commands.
func TestVolumePathRejectsExplicitEndpointURL(t *testing.T) {
	t.Parallel()
	e := env.With(env.DisableEvents, "1").WithHome(t.TempDir())

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), e, "volume", "path", "--endpoint-url", "http://localhost:4566")
	require.Error(t, err)
	snap.Match(t, sanitizeOutput(stdout))
}

// TestStartRejectsExplicitEndpointURL proves `start` (previously missing from
// the exclusion list entirely) rejects an explicit --endpoint-url before ever
// touching Docker, exactly like logs/stop/restart/volume.
func TestStartRejectsExplicitEndpointURL(t *testing.T) {
	t.Parallel()
	e := env.With(env.DisableEvents, "1").WithHome(t.TempDir())
	e = append(e, unreachableDockerHost)

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), e, "start", "--endpoint-url", "http://localhost:4566")
	require.Error(t, err)
	snap.Match(t, sanitizeOutput(stdout))
}

// The following tests prove the corrected behavior from design.md's Decision
// 5: an *ambient* LSTK_ENDPOINT_URL/AWS_ENDPOINT_URL (no explicit flag) is now
// rejected too, not silently ignored — for every Docker-lifecycle/filesystem
// command, including the newly-added `start`. None of these touch Docker (a
// broken DOCKER_HOST proves it), matching the requirement that rejection
// never depends on checking local emulator state first.

func TestLogsRejectsAmbientEndpointURL(t *testing.T) {
	t.Parallel()
	e := env.With(env.DisableEvents, "1").WithHome(t.TempDir()).With("LSTK_ENDPOINT_URL", "http://localhost:4566")
	e = append(e, unreachableDockerHost)

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), e, "logs")
	require.Error(t, err)
	snap.Match(t, sanitizeOutput(stdout))
}

func TestStopRejectsAmbientEndpointURL(t *testing.T) {
	t.Parallel()
	e := env.With(env.DisableEvents, "1").WithHome(t.TempDir()).With("LSTK_ENDPOINT_URL", "http://localhost:4566")
	e = append(e, unreachableDockerHost)

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), e, "stop")
	require.Error(t, err)
	snap.Match(t, sanitizeOutput(stdout))
}

func TestRestartRejectsAmbientEndpointURL(t *testing.T) {
	t.Parallel()
	e := env.With(env.DisableEvents, "1").WithHome(t.TempDir()).With("AWS_ENDPOINT_URL", "http://localhost:4566")
	e = append(e, unreachableDockerHost)

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), e, "restart")
	require.Error(t, err)
	snap.Match(t, sanitizeOutput(stdout))
}

func TestVolumePathRejectsAmbientEndpointURL(t *testing.T) {
	t.Parallel()
	e := env.With(env.DisableEvents, "1").WithHome(t.TempDir()).With("LSTK_ENDPOINT_URL", "http://localhost:4566")

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), e, "volume", "path")
	require.Error(t, err)
	snap.Match(t, sanitizeOutput(stdout))
}

func TestVolumeClearRejectsAmbientEndpointURL(t *testing.T) {
	t.Parallel()
	e := env.With(env.DisableEvents, "1").WithHome(t.TempDir()).With("LSTK_ENDPOINT_URL", "http://localhost:4566")

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), e, "volume", "clear")
	require.Error(t, err)
	snap.Match(t, sanitizeOutput(stdout))
}

func TestStartRejectsAmbientEndpointURL(t *testing.T) {
	t.Parallel()
	e := env.With(env.DisableEvents, "1").WithHome(t.TempDir()).With("LSTK_ENDPOINT_URL", "http://localhost:4566")
	e = append(e, unreachableDockerHost)

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), e, "start")
	require.Error(t, err)
	snap.Match(t, sanitizeOutput(stdout))
}

// TestSnapshotShowIgnoresEndpointURL proves `snapshot show` silently ignores
// --endpoint-url (it only ever talks to the LocalStack platform API, which is
// account-scoped rather than emulator-scoped) rather than rejecting it: with
// the platform API pointed at an unreachable address, the command still
// attempts — and fails on — that platform call, not on flag validation.
func TestSnapshotShowIgnoresEndpointURL(t *testing.T) {
	t.Parallel()
	e := env.With(env.DisableEvents, "1").WithHome(t.TempDir()).With(env.APIEndpoint, "http://127.0.0.1:1")

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "snapshot", "show", "pod:my-baseline", "--endpoint-url", "http://localhost:4566")
	require.Error(t, err)
	assert.NotContains(t, stdout, "does not support --endpoint-url")
	assert.NotContains(t, stderr, "does not support --endpoint-url")
}

// TestSnapshotListBareIgnoresEndpointURL mirrors the show case for `snapshot
// list` with no s3:// argument.
func TestSnapshotListBareIgnoresEndpointURL(t *testing.T) {
	t.Parallel()
	e := env.With(env.DisableEvents, "1").WithHome(t.TempDir()).With(env.APIEndpoint, "http://127.0.0.1:1")

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

	e := env.With(env.DisableEvents, "1").WithHome(t.TempDir())
	e = append(e, unreachableDockerHost)

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "--endpoint-url", srv.URL, "status")
	require.NoError(t, err, "stderr: %s", stderr)
	// The snapshot pins the reduced remote-status card: running headline and
	// endpoint only — no Container/Uptime lines, which need local Docker.
	snap.Match(t, sanitizeOutput(stdout))
}

// TestStatusEndpointURLShowsResources proves the resources bug fix: `status`
// against an externally-managed endpoint reports deployed resources for an
// AWS-typed target exactly as it does for a Docker-managed one — deployed
// resources are an ordinary emulator API call (/_localstack/resources), not
// Docker-derived, so there was no reason the initial cut of this change
// omitted them entirely.
func TestStatusEndpointURLShowsResources(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/_localstack/health":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"version":  "3.0.2",
				"services": map[string]string{"s3": "available", "sqs": "available"},
			})
		case "/_localstack/resources":
			w.Header().Set("Content-Type", "application/x-ndjson")
			_, _ = fmt.Fprintln(w, `{"AWS::S3::Bucket": [{"region_name": "us-east-1", "account_id": "000000000000", "id": "my-test-bucket"}]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	e := env.With(env.DisableEvents, "1").WithHome(t.TempDir())
	e = append(e, unreachableDockerHost)

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "--endpoint-url", srv.URL, "status")
	require.NoError(t, err, "stderr: %s", stderr)
	snap.Match(t, sanitizeOutput(stdout))
}

// TestStatusEndpointURLInteractiveRendersTUI proves `status` against an
// externally-managed endpoint renders through the same Bubble Tea TUI as the
// Docker-managed path when attached to a terminal. It used to hardcode the
// plain sink regardless of interactive mode, so `--endpoint-url status` came
// out unstyled and without the blank lines the TUI puts around the resource
// summary — visibly different output for the same command.
func TestStatusEndpointURLInteractiveRendersTUI(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/_localstack/health":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"version":  "3.0.2",
				"services": map[string]string{"s3": "available"},
			})
		case "/_localstack/resources":
			w.Header().Set("Content-Type", "application/x-ndjson")
			_, _ = fmt.Fprintln(w, `{"AWS::S3::Bucket": [{"region_name": "us-east-1", "account_id": "000000000000", "id": "my-test-bucket"}]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	e := append(env.Environ(testEnvWithHome(t.TempDir(), "")).With(env.DisableEvents, "1"), unreachableDockerHost)
	p := startLstkInPTY(t, testContext(t), e, "--endpoint-url", srv.URL, "status")
	_, err := p.wait()
	require.NoError(t, err)

	lines := ptyLines(p.out.String())
	summary := -1
	for i, line := range lines {
		if strings.Contains(line, "resources ·") {
			summary = i
			break
		}
	}
	require.NotEqual(t, -1, summary, "resource summary should be rendered, got:\n%s", strings.Join(lines, "\n"))
	require.Greater(t, summary, 0)
	assert.Empty(t, lines[summary-1], "TUI puts a blank line before the resource summary")
	assert.Empty(t, lines[summary+1], "TUI puts a blank line after the resource summary")

	joined := strings.Join(lines, "\n")
	assert.Contains(t, joined, "my-test-bucket")
	assert.NotContains(t, joined, "Container:")
	assert.NotContains(t, joined, "Uptime:")
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

	e := env.With(env.DisableEvents, "1").WithHome(t.TempDir())
	e = append(e, unreachableDockerHost)

	_, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "--endpoint-url", notLocalStack.URL, "status")
	require.Error(t, err)
	// The snapshot pins the full error, including that it never suggests
	// `lstk start` — the target is an emulator lstk did not start.
	snap.Match(t, sanitizeOutput(stderr))
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
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(t.TempDir()).
		With("AWS_ENDPOINT_URL", srv.URL)
	e = append(e, unreachableDockerHost)

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "cdk", "deploy", "MyStack")
	require.NoError(t, err, "stderr: %s", stderr)
	snap.Match(t, sanitizeOutput(stdout))
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
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(t.TempDir())
	e = append(e, unreachableDockerHost)

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), e, "--endpoint-url", azureLike.URL, "cdk", "deploy", "MyStack")
	require.Error(t, err)
	snap.Match(t, sanitizeOutput(stdout))
}
