package integration_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/creack/pty"
	"github.com/localstack/lstk/internal/must"
	"github.com/localstack/lstk/internal/snap"
	"github.com/localstack/lstk/test/integration/env"
)

// unreachableDockerHost points DOCKER_HOST at a socket that can't possibly
// exist, so any code path that actually tries to talk to Docker fails
// immediately and loudly — a clean way to prove a command took the
// --endpoint-url path without touching Docker at all, without needing a real
// Docker daemon to be unavailable in the test environment.
const unreachableDockerHost = "DOCKER_HOST=unix:///nonexistent/docker.sock"

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
	if runtime.GOOS == "windows" {
		t.Skip("fake aws script not supported on Windows")
	}
	dir := t.TempDir()
	script := `#!/bin/sh
echo "ENDPOINT:$2"
shift 2
echo "ARGS:$@"
`
	must.NoError(t, os.WriteFile(filepath.Join(dir, "aws"), []byte(script), 0755))
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
	must.NoError(t, err, "stderr: %s", stderr)
	must.Contains(t, stdout, "ENDPOINT:"+srv.URL)
	must.Contains(t, stdout, "ARGS:s3 ls")
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
	must.NoError(t, err, "stderr: %s", stderr)
	// lstk's own resolution used the pre-command mock — proven by succeeding
	// at all with DOCKER_HOST broken.
	must.Contains(t, stdout, "ENDPOINT:"+srv.URL)
	// The user's own post-command --endpoint-url reached the wrapped aws
	// binary untouched, appearing in its forwarded args.
	must.Contains(t, stdout, "ARGS:--endpoint-url http://127.0.0.1:9 s3 ls")
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
	must.Error(t, err)
	must.Contains(t, stdout, "logs")
	must.Contains(t, stdout, "does not support --endpoint-url")
}

// TestVolumePathRejectsExplicitEndpointURL mirrors the logs case for the
// other Docker-lifecycle/filesystem commands.
func TestVolumePathRejectsExplicitEndpointURL(t *testing.T) {
	t.Parallel()
	e := env.With(env.DisableEvents, "1").With(env.Home, t.TempDir())

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), e, "volume", "path", "--endpoint-url", "http://localhost:4566")
	must.Error(t, err)
	must.Contains(t, stdout, "does not support --endpoint-url")
}

// TestStartRejectsExplicitEndpointURL proves `start` (previously missing from
// the exclusion list entirely) rejects an explicit --endpoint-url before ever
// touching Docker, exactly like logs/stop/restart/volume.
func TestStartRejectsExplicitEndpointURL(t *testing.T) {
	t.Parallel()
	e := env.With(env.DisableEvents, "1").With(env.Home, t.TempDir())
	e = append(e, unreachableDockerHost)

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), e, "start", "--endpoint-url", "http://localhost:4566")
	must.Error(t, err)
	must.Contains(t, stdout, "start")
	must.Contains(t, stdout, "does not support --endpoint-url")
}

// The following tests prove the corrected behavior from design.md's Decision
// 5: an *ambient* LSTK_ENDPOINT_URL/AWS_ENDPOINT_URL (no explicit flag) is now
// rejected too, not silently ignored — for every Docker-lifecycle/filesystem
// command, including the newly-added `start`. None of these touch Docker (a
// broken DOCKER_HOST proves it), matching the requirement that rejection
// never depends on checking local emulator state first.

func TestLogsRejectsAmbientEndpointURL(t *testing.T) {
	t.Parallel()
	e := env.With(env.DisableEvents, "1").With(env.Home, t.TempDir()).With("LSTK_ENDPOINT_URL", "http://localhost:4566")
	e = append(e, unreachableDockerHost)

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), e, "logs")
	must.Error(t, err)
	must.Contains(t, stdout, "logs")
	must.Contains(t, stdout, "does not support LSTK_ENDPOINT_URL")
	must.Contains(t, stdout, "LSTK_ENDPOINT_URL is set")
}

func TestStopRejectsAmbientEndpointURL(t *testing.T) {
	t.Parallel()
	e := env.With(env.DisableEvents, "1").With(env.Home, t.TempDir()).With("LSTK_ENDPOINT_URL", "http://localhost:4566")
	e = append(e, unreachableDockerHost)

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), e, "stop")
	must.Error(t, err)
	must.Contains(t, stdout, "does not support LSTK_ENDPOINT_URL")
}

func TestRestartRejectsAmbientEndpointURL(t *testing.T) {
	t.Parallel()
	e := env.With(env.DisableEvents, "1").With(env.Home, t.TempDir()).With("AWS_ENDPOINT_URL", "http://localhost:4566")
	e = append(e, unreachableDockerHost)

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), e, "restart")
	must.Error(t, err)
	must.Contains(t, stdout, "does not support AWS_ENDPOINT_URL")
	must.Contains(t, stdout, "AWS_ENDPOINT_URL is set")
}

func TestVolumePathRejectsAmbientEndpointURL(t *testing.T) {
	t.Parallel()
	e := env.With(env.DisableEvents, "1").With(env.Home, t.TempDir()).With("LSTK_ENDPOINT_URL", "http://localhost:4566")

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), e, "volume", "path")
	must.Error(t, err)
	must.Contains(t, stdout, "does not support LSTK_ENDPOINT_URL")
}

func TestVolumeClearRejectsAmbientEndpointURL(t *testing.T) {
	t.Parallel()
	e := env.With(env.DisableEvents, "1").With(env.Home, t.TempDir()).With("LSTK_ENDPOINT_URL", "http://localhost:4566")

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), e, "volume", "clear")
	must.Error(t, err)
	must.Contains(t, stdout, "does not support LSTK_ENDPOINT_URL")
}

func TestStartRejectsAmbientEndpointURL(t *testing.T) {
	t.Parallel()
	e := env.With(env.DisableEvents, "1").With(env.Home, t.TempDir()).With("LSTK_ENDPOINT_URL", "http://localhost:4566")
	e = append(e, unreachableDockerHost)

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), e, "start")
	must.Error(t, err)
	must.Contains(t, stdout, "start")
	must.Contains(t, stdout, "does not support LSTK_ENDPOINT_URL")
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
	must.Error(t, err)
	must.NotContains(t, stdout, "does not support --endpoint-url")
	must.NotContains(t, stderr, "does not support --endpoint-url")
}

// TestSnapshotListBareIgnoresEndpointURL mirrors the show case for `snapshot
// list` with no s3:// argument.
func TestSnapshotListBareIgnoresEndpointURL(t *testing.T) {
	t.Parallel()
	e := env.With(env.DisableEvents, "1").With(env.Home, t.TempDir()).With(env.APIEndpoint, "http://127.0.0.1:1")

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "snapshot", "list", "--endpoint-url", "http://localhost:4566")
	must.Error(t, err)
	must.NotContains(t, stdout, "does not support --endpoint-url")
	must.NotContains(t, stderr, "does not support --endpoint-url")
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
	must.NoError(t, err, "stderr: %s", stderr)
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

	e := env.With(env.DisableEvents, "1").With(env.Home, t.TempDir())
	e = append(e, unreachableDockerHost)

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "--endpoint-url", srv.URL, "status")
	must.NoError(t, err, "stderr: %s", stderr)
	must.Contains(t, stdout, "SERVICE")
	must.Contains(t, stdout, "RESOURCE")
	must.Contains(t, stdout, "S3")
	must.Contains(t, stdout, "my-test-bucket")
}

// TestStatusEndpointURLInteractiveRendersTUI proves `status` against an
// externally-managed endpoint renders through the same Bubble Tea TUI as the
// Docker-managed path when attached to a terminal. It used to hardcode the
// plain sink regardless of interactive mode, so `--endpoint-url status` came
// out unstyled and without the blank lines the TUI puts around the resource
// summary — visibly different output for the same command.
func TestStatusEndpointURLInteractiveRendersTUI(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("PTY not supported on Windows")
	}
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

	binPath, err := filepath.Abs(binaryPath())
	must.NoError(t, err)

	cmd := exec.CommandContext(testContext(t), binPath, "--endpoint-url", srv.URL, "status")
	cmd.Env = append(env.Environ(testEnvWithHome(t.TempDir(), "")).With(env.DisableEvents, "1"), unreachableDockerHost)
	ptmx, err := pty.Start(cmd)
	must.NoError(t, err, "failed to start command in PTY")
	t.Cleanup(func() { _ = ptmx.Close() })

	out := &syncBuffer{}
	outputCh := make(chan struct{})
	go func() {
		_, _ = io.Copy(out, ptmx)
		close(outputCh)
	}()
	must.NoError(t, cmd.Wait())
	<-outputCh

	lines := ptyLines(out.String())
	summary := -1
	for i, line := range lines {
		if strings.Contains(line, "resources ·") {
			summary = i
			break
		}
	}
	must.NotEq(t, -1, summary, "resource summary should be rendered, got:\n%s", strings.Join(lines, "\n"))
	must.Greater(t, summary, 0)
	must.Empty(t, lines[summary-1], "TUI puts a blank line before the resource summary")
	must.Empty(t, lines[summary+1], "TUI puts a blank line after the resource summary")

	joined := strings.Join(lines, "\n")
	must.Contains(t, joined, "my-test-bucket")
	must.NotContains(t, joined, "Container:")
	must.NotContains(t, joined, "Uptime:")
}

// ptyLines splits raw PTY output into display lines, dropping ANSI escape
// sequences and carriage returns so tests can assert on layout (blank lines,
// ordering) without depending on whether the terminal advertised colour.
func ptyLines(raw string) []string {
	stripped := ansiEscape.ReplaceAllString(raw, "")
	stripped = strings.ReplaceAll(stripped, "\r", "")
	lines := strings.Split(stripped, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	return lines
}

var ansiEscape = regexp.MustCompile(`\x1b(\[[0-9;?]*[ -/]*[@-~]|\][^\x07\x1b]*(\x07|\x1b\\)|[@-Z\\-_])`)

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
	must.Error(t, err)
	must.Contains(t, stderr, "could not reach")
	must.NotContains(t, stderr, "lstk start")
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
	must.NoError(t, err, "stderr: %s", stderr)
	must.Contains(t, stdout, fmt.Sprintf("ENV_AWS_ENDPOINT_URL=%s", srv.URL))
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
	must.Error(t, err)
	must.Contains(t, stdout, "requires the AWS emulator")
	must.Contains(t, stdout, "Azure")
}
