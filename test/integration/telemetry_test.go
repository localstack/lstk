package integration_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockAnalyticsServer returns a test server that records received analytics events.
// The returned channel receives one value per event (unwrapped from the events array).
func mockAnalyticsServer(t *testing.T) (*httptest.Server, <-chan map[string]any) {
	t.Helper()
	ch := make(chan map[string]any, 10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var req struct {
			Events []map[string]any `json:"events"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		for _, event := range req.Events {
			ch <- event
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv, ch
}

func TestStartCommandSendsTelemetryEvent(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Pre-start a container so both invocations exit immediately without a real token.
	startTestContainer(t, ctx)

	analyticsSrv, events := mockAnalyticsServer(t)

	runCmd := func(t *testing.T, args ...string) {
		t.Helper()
		cmd := exec.CommandContext(ctx, binaryPath(), args...)
		cmd.Env = env.Environ(testEnvWithHome(t.TempDir(), "")).
			With(env.AuthToken, "fake-token").
			With(env.AnalyticsEndpoint, analyticsSrv.URL)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "lstk %v failed: %s", args, out)
	}

	t.Run("lstk start emits command=start", func(t *testing.T) {
		runCmd(t, "start")

		select {
		case event := <-events:
			assert.Equal(t, "lstk_command", event["name"])

			metadata, ok := event["metadata"].(map[string]any)
			require.True(t, ok)
			_, err := uuid.Parse(metadata["session_id"].(string))
			assert.NoError(t, err, "session_id should be a valid UUID")
			_, err = time.Parse("2006-01-02 15:04:05.000000", metadata["client_time"].(string))
			assert.NoError(t, err, "client_time should match expected format")

			payload, ok := event["payload"].(map[string]any)
			require.True(t, ok)
			assert.NotEmpty(t, payload["machine_id"], "machine_id should be present")
			assert.Equal(t, os.Getenv("CI") != "", payload["is_ci"])

			environment, ok := payload["environment"].(map[string]any)
			require.True(t, ok)
			assert.NotEmpty(t, environment["lstk_version"])
			assert.Equal(t, runtime.GOOS, environment["os"])
			assert.Equal(t, runtime.GOARCH, environment["arch"])

			params, ok := payload["parameters"].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, "start", params["command"])
			assert.Equal(t, false, params["proxied"], "lstk's own command is out of the proxy population")

			result, ok := payload["result"].(map[string]any)
			require.True(t, ok)
			assert.NotContains(t, result, "proxy_exit_code", "no wrapped tool ran")
			assert.Equal(t, false, result["cancelled"])
			assert.InDelta(t, 0, result["exit_code"], 0)
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for telemetry event")
		}
	})

	t.Run("lstk (no subcommand) emits command=start", func(t *testing.T) {
		runCmd(t)
		assertCommandTelemetry(t, events, "start", 0)
	})
}

func TestStopCommandSendsTelemetryEvents(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)

	analyticsSrv, events := mockAnalyticsServer(t)

	_, stderr, err := runLstk(t, ctx, "", env.Environ(testEnvWithHome(t.TempDir(), "")).
		With(env.AuthToken, "fake-token").
		With(env.AnalyticsEndpoint, analyticsSrv.URL), "stop")
	require.NoError(t, err, "lstk stop failed: %s", stderr)
	requireExitCode(t, 0, err)

	// Collect both the lstk_lifecycle and lstk_command events (order not guaranteed).
	byName := make(map[string]map[string]any)
	deadline := time.After(3 * time.Second)
	for len(byName) < 2 {
		select {
		case event := <-events:
			name, _ := event["name"].(string)
			byName[name] = event
		case <-deadline:
			t.Fatalf("timed out waiting for telemetry events; received: %v", byName)
		}
	}

	lifecycle, ok := byName["lstk_lifecycle"]
	require.True(t, ok, "expected lstk_lifecycle event")
	lp := lifecycle["payload"].(map[string]any)
	assert.Equal(t, "stop", lp["event_type"])
	assert.Equal(t, "aws", lp["emulator"])

	command, ok := byName["lstk_command"]
	require.True(t, ok, "expected lstk_command event")
	cp := command["payload"].(map[string]any)
	params, ok := cp["parameters"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "stop", params["command"])
	result, ok := cp["result"].(map[string]any)
	require.True(t, ok)
	assert.InDelta(t, 0, result["exit_code"], 0)
}

func TestStartCommandSucceedsWhenAnalyticsEndpointUnreachable(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	startTestContainer(t, ctx)

	cmd := exec.CommandContext(ctx, binaryPath(), "start")
	cmd.Env = env.Environ(testEnvWithHome(t.TempDir(), "")).
		With(env.AuthToken, "fake-token").
		With(env.AnalyticsEndpoint, "http://127.0.0.1:1")
	out, err := cmd.CombinedOutput()

	require.NoError(t, err, "lstk start should succeed even when analytics endpoint is unreachable: %s", out)
	requireExitCode(t, 0, err)
}

func TestStartCommandDoesNotSendTelemetryWhenDisabled(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	startTestContainer(t, ctx)

	analyticsSrv, events := mockAnalyticsServer(t)

	cmd := exec.CommandContext(ctx, binaryPath(), "start")
	cmd.Env = env.Environ(testEnvWithHome(t.TempDir(), "")).
		With(env.AuthToken, "fake-token").
		With(env.AnalyticsEndpoint, analyticsSrv.URL).
		With(env.DisableEvents, "1")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "lstk start failed: %s", out)
	requireExitCode(t, 0, err)

	// Wait long enough that a goroutine would have fired if enabled.
	select {
	case event := <-events:
		t.Fatalf("unexpected telemetry event received: %v", event)
	case <-time.After(time.Second):
		// No event received — correct.
	}
}

// commandEventParts unpacks an lstk_command event into its parameters and
// result blocks.
func commandEventParts(t *testing.T, event map[string]any) (params, result map[string]any) {
	t.Helper()
	payload, ok := event["payload"].(map[string]any)
	require.True(t, ok, "payload: %v", event)
	params, ok = payload["parameters"].(map[string]any)
	require.True(t, ok, "parameters: %v", payload)
	result, ok = payload["result"].(map[string]any)
	require.True(t, ok, "result: %v", payload)
	return params, result
}

// proxyEnviron is the shared environment of the no-Docker proxy tests:
// analytics at the mock server, the fake tool first on PATH, Docker unreachable.
func proxyEnviron(t *testing.T, analyticsURL, fakeBinDir string) []string {
	t.Helper()
	environ := env.Environ(testEnvWithHome(t.TempDir(), "")).
		With(env.AnalyticsEndpoint, analyticsURL).
		With(env.Path, fakeBinDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return append(environ, unreachableDockerHost)
}

// DEVX-1003/DEVX-1004: a proxied `lstk aws` failure records the wrapped CLI's
// real exit code as exit_code and as proxy_exit_code, plus the leading
// service/operation tokens.
func TestAWSProxyTelemetryRecordsToolExit(t *testing.T) {
	t.Parallel()

	emulatorSrv := awsHealthServer(t)
	defer emulatorSrv.Close()
	analyticsSrv, events := mockAnalyticsServer(t)

	// Fake aws on PATH exiting like the real CLI does on a usage error, so the
	// test needs neither the AWS CLI installed nor a real malformed request.
	fakeBinDir := writeFakeTool(t, "aws", fakeToolConfig{ExitCode: 252})

	_, _, err := runLstk(t, testContext(t), "", proxyEnviron(t, analyticsSrv.URL, fakeBinDir),
		"--endpoint-url", emulatorSrv.URL, "aws", "s3", "lss")
	require.Error(t, err)
	requireExitCode(t, 252, err)

	params, result := commandEventParts(t, receiveEventByName(t, events, "lstk_command"))
	assert.Equal(t, "aws", params["command"])
	assert.Equal(t, "s3 lss", params["subcommand"])
	assert.Equal(t, true, params["proxied"])
	assert.InDelta(t, 252, result["exit_code"], 0)
	assert.InDelta(t, 252, result["proxy_exit_code"], 0, "the AWS CLI ran and exited non-zero, so the failure is not lstk's")
	assert.Equal(t, false, result["cancelled"])
}

// The row the failure-rate denominator needs: a proxy invocation whose tool
// succeeded is still proxied and records the tool's 0.
func TestAWSProxyTelemetryRecordsSuccessfulToolExit(t *testing.T) {
	t.Parallel()

	emulatorSrv := awsHealthServer(t)
	defer emulatorSrv.Close()
	analyticsSrv, events := mockAnalyticsServer(t)

	fakeBinDir := writeFakeTool(t, "aws", fakeToolConfig{Stdout: []string{"2026-09-23 my-bucket"}})

	_, _, err := runLstk(t, testContext(t), "", proxyEnviron(t, analyticsSrv.URL, fakeBinDir),
		"--endpoint-url", emulatorSrv.URL, "aws", "s3", "ls")
	require.NoError(t, err)

	params, result := commandEventParts(t, receiveEventByName(t, events, "lstk_command"))
	assert.Equal(t, "aws", params["command"])
	assert.Equal(t, true, params["proxied"])
	assert.InDelta(t, 0, result["exit_code"], 0)
	require.Contains(t, result, "proxy_exit_code", "presence is how the pipe knows the tool ran")
	assert.InDelta(t, 0, result["proxy_exit_code"], 0)
}

// DEVX-1004: a preflight failure shares the command name and exit-code space
// with the wrapped tool's own failures. The invocation is still proxied, but
// no tool ran, so proxy_exit_code must be absent, not 0.
func TestProxyPreflightFailureTelemetryHasNoToolExit(t *testing.T) {
	t.Parallel()

	analyticsSrv, events := mockAnalyticsServer(t)

	// Gets the run past the installed check so it fails on preflight instead.
	fakeBinDir := writeFakeTool(t, "aws", fakeToolConfig{})

	_, _, err := runLstk(t, testContext(t), "", proxyEnviron(t, analyticsSrv.URL, fakeBinDir), "aws", "s3", "ls")
	require.Error(t, err)
	requireExitCode(t, 1, err)

	params, result := commandEventParts(t, receiveEventByName(t, events, "lstk_command"))
	assert.Equal(t, "aws", params["command"])
	assert.Equal(t, true, params["proxied"])
	assert.InDelta(t, 1, result["exit_code"], 0)
	assert.NotContains(t, result, "proxy_exit_code", "an unreachable runtime is lstk's own failure; no tool ran")
	assert.Equal(t, false, result["cancelled"])
}

// DEVX-1004: `setup azure` and interception shell out to `az` too, but lstk
// composed those calls; recording a tool exit would hide an lstk bug as the
// user's. The command runs under the `az` proxy without being proxied.
func TestLstkOrchestratedAzFailureTelemetryIsNotProxied(t *testing.T) {
	t.Parallel()

	analyticsSrv, events := mockAnalyticsServer(t)

	fakeBinDir := writeFakeTool(t, "az", fakeToolConfig{
		Stderr:   []string{"ERROR: fake az failure"},
		ExitCode: 1,
	})

	environ := env.Environ(testEnvWithHome(t.TempDir(), "")).
		With(env.AnalyticsEndpoint, analyticsSrv.URL).
		With(env.Path, fakeBinDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	_, stderr, err := runLstk(t, testContext(t), "", environ, "az", "stop-interception")
	require.Error(t, err)
	require.Contains(t, stderr, "fake az failure", "the failure must come from the az call lstk composed, not an earlier guard")

	params, result := commandEventParts(t, receiveEventByName(t, events, "lstk_command"))
	assert.Equal(t, "az stop-interception", params["command"])
	assert.Equal(t, false, params["proxied"])
	assert.InDelta(t, 1, result["exit_code"], 0)
	assert.NotContains(t, result, "proxy_exit_code", "lstk composed this az call, so its failure is lstk's")
}

// The other half of the azurecli Exec/Run split: `lstk az` forwards the user's
// own args, so a refactor must not unmark it while keeping Run correct.
func TestAzPassthroughFailureTelemetryRecordsToolExit(t *testing.T) {
	t.Parallel()

	emulatorSrv := azureHealthServer(t)
	analyticsSrv, events := mockAnalyticsServer(t)
	configPath := azureConfigWithSetupMarker(t)

	fakeBinDir := writeFakeTool(t, "az", fakeToolConfig{ExitCode: 3})
	environ := env.Environ(testEnvWithHome(t.TempDir(), "")).
		With(env.AnalyticsEndpoint, analyticsSrv.URL).
		With(env.Path, fakeBinDir)
	environ = append(environ, unreachableDockerHost)

	_, _, err := runLstk(t, testContext(t), t.TempDir(), environ,
		"--endpoint-url", emulatorSrv.URL, "--config", configPath, "--non-interactive",
		"az", "group", "lst")
	require.Error(t, err)
	requireExitCode(t, 3, err)

	params, result := commandEventParts(t, receiveEventByName(t, events, "lstk_command"))
	assert.Equal(t, "az", params["command"])
	assert.Equal(t, true, params["proxied"])
	assert.InDelta(t, 3, result["exit_code"], 0)
	assert.InDelta(t, 3, result["proxy_exit_code"], 0, "the user typed these az args, so the exit is theirs")
}

// The tool is not installed: still a proxy invocation, but nothing ran.
func TestProxyToolNotInstalledTelemetryHasNoToolExit(t *testing.T) {
	t.Parallel()

	emulatorSrv := awsHealthServer(t)
	defer emulatorSrv.Close()
	analyticsSrv, events := mockAnalyticsServer(t)

	// PATH is an empty temp dir, so `aws` resolves nowhere.
	environ := env.Environ(testEnvWithHome(t.TempDir(), "")).
		With(env.AnalyticsEndpoint, analyticsSrv.URL).
		With(env.Path, t.TempDir())
	environ = append(environ, unreachableDockerHost)

	stdout, _, err := runLstk(t, testContext(t), "", environ, "--endpoint-url", emulatorSrv.URL, "aws", "s3", "ls")
	require.Error(t, err)
	requireExitCode(t, 1, err)
	require.Contains(t, stdout, "aws CLI not found in PATH")

	params, result := commandEventParts(t, receiveEventByName(t, events, "lstk_command"))
	assert.Equal(t, "aws", params["command"])
	assert.Equal(t, true, params["proxied"])
	assert.InDelta(t, 1, result["exit_code"], 0)
	assert.NotContains(t, result, "proxy_exit_code")
}

// DEVX-1004: with stdin, stdout and stderr all terminals, `lstk aws` runs the
// tool in a PTY and pumps Ctrl-C to it as a byte, so only the child gets
// SIGINT and lstk's own signal context never fires. The forwarded interrupt is
// the only trace, and it must still record as cancelled.
func TestInteractiveCtrlCOnProxyRecordsCancellation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("proc.RunInPTY is unix-only")
	}
	t.Parallel()

	emulatorSrv := awsHealthServer(t)
	defer emulatorSrv.Close()
	analyticsSrv, events := mockAnalyticsServer(t)

	// The aws CLI traps SIGINT and exits 130 rather than dying by the signal,
	// which is what makes the exit code alone indistinguishable from a failure.
	fakeBinDir := writeFakeTool(t, "aws", fakeToolConfig{Stdout: []string{"READY"}, SleepSeconds: 30, TrapExitCode: 130})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	p := startLstkInPTY(t, ctx, proxyEnviron(t, analyticsSrv.URL, fakeBinDir),
		"--endpoint-url", emulatorSrv.URL, "aws", "s3", "ls")
	p.waitForOutput("READY")
	p.write("\x03")
	_, err := p.wait()
	requireExitCode(t, 130, err)

	params, result := commandEventParts(t, receiveEventByName(t, events, "lstk_command"))
	assert.Equal(t, true, params["proxied"])
	assert.InDelta(t, 130, result["proxy_exit_code"], 0)
	assert.Equal(t, true, result["cancelled"], "Ctrl-C forwarded through the PTY is the user's interruption, not the tool's failure")
}

// The why axis: a user's own input error on a proxy command is lstk's failure
// to complete the invocation, but not an lstk defect. The code the user saw
// rides the event so the pipe can tell the two apart.
func TestProxyValidationErrorTelemetryIsClassifiedAsUsage(t *testing.T) {
	t.Parallel()

	emulatorSrv := awsHealthServer(t)
	defer emulatorSrv.Close()
	analyticsSrv, events := mockAnalyticsServer(t)
	fakeBinDir := writeFakeTool(t, "aws", fakeToolConfig{})

	_, _, err := runLstk(t, testContext(t), "", proxyEnviron(t, analyticsSrv.URL, fakeBinDir),
		"--endpoint-url", emulatorSrv.URL, "aws", "--account", "123", "s3", "ls")
	require.Error(t, err)

	params, result := commandEventParts(t, receiveEventByName(t, events, "lstk_command"))
	assert.Equal(t, true, params["proxied"])
	assert.NotContains(t, result, "proxy_exit_code")
	assert.Equal(t, "VALIDATION_ERROR", result["error_code"])
	assert.Equal(t, "USAGE", result["error_category"])
}

func TestProxyPreflightDockerDownTelemetryIsClassifiedAsRuntime(t *testing.T) {
	t.Parallel()

	analyticsSrv, events := mockAnalyticsServer(t)
	fakeBinDir := writeFakeTool(t, "aws", fakeToolConfig{})

	_, _, err := runLstk(t, testContext(t), "", proxyEnviron(t, analyticsSrv.URL, fakeBinDir), "aws", "s3", "ls")
	require.Error(t, err)

	_, result := commandEventParts(t, receiveEventByName(t, events, "lstk_command"))
	assert.Equal(t, "RUNTIME_UNAVAILABLE", result["error_code"])
	assert.Equal(t, "RUNTIME", result["error_category"])
}

// The endpoint-preflight failures are not the user's arguments: an unreachable
// endpoint is connectivity, a wrong emulator type behind it is an emulator
// mismatch. Both used to share VALIDATION_ERROR with `--account 123`.
func TestUnreachableEndpointTelemetryIsClassifiedAsNetwork(t *testing.T) {
	t.Parallel()

	analyticsSrv, events := mockAnalyticsServer(t)
	fakeBinDir := writeFakeTool(t, "aws", fakeToolConfig{})

	_, _, err := runLstk(t, testContext(t), "", proxyEnviron(t, analyticsSrv.URL, fakeBinDir),
		"--endpoint-url", "http://127.0.0.1:1", "aws", "s3", "ls")
	require.Error(t, err)

	_, result := commandEventParts(t, receiveEventByName(t, events, "lstk_command"))
	assert.Equal(t, "NETWORK_ERROR", result["error_code"])
	assert.Equal(t, "RUNTIME", result["error_category"])
}

func TestWrongEmulatorTypeEndpointTelemetryIsClassifiedAsEmulator(t *testing.T) {
	t.Parallel()

	emulatorSrv := azureHealthServer(t)
	analyticsSrv, events := mockAnalyticsServer(t)
	fakeBinDir := writeFakeTool(t, "aws", fakeToolConfig{})

	_, _, err := runLstk(t, testContext(t), "", proxyEnviron(t, analyticsSrv.URL, fakeBinDir),
		"--endpoint-url", emulatorSrv.URL, "aws", "s3", "ls")
	require.Error(t, err)

	_, result := commandEventParts(t, receiveEventByName(t, events, "lstk_command"))
	assert.Equal(t, "EMULATOR_WRONG_TYPE", result["error_code"])
	assert.Equal(t, "EMULATOR", result["error_category"])
}

// lstk's own az call failing is lstk's failure with no better code than
// INTERNAL_ERROR; the point is that it is classified at all, so the
// unclassified share does not hide it.
func TestLstkOrchestratedAzFailureTelemetryIsClassified(t *testing.T) {
	t.Parallel()

	analyticsSrv, events := mockAnalyticsServer(t)
	fakeBinDir := writeFakeTool(t, "az", fakeToolConfig{Stderr: []string{"ERROR: fake az failure"}, ExitCode: 1})
	environ := env.Environ(testEnvWithHome(t.TempDir(), "")).
		With(env.AnalyticsEndpoint, analyticsSrv.URL).
		With(env.Path, fakeBinDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	_, _, err := runLstk(t, testContext(t), "", environ, "az", "stop-interception")
	require.Error(t, err)

	_, result := commandEventParts(t, receiveEventByName(t, events, "lstk_command"))
	assert.Equal(t, "INTERNAL_ERROR", result["error_code"])
	assert.Equal(t, "INTERNAL", result["error_category"])
}

// `setup azure` probes the emulator before touching az; with none running
// the domain function returns a classified error for the command layer to
// render (output.WithCode), which must reach telemetry without a sink.
func TestSetupAzureUnreachableEmulatorTelemetryIsClassified(t *testing.T) {
	t.Parallel()

	analyticsSrv, events := mockAnalyticsServer(t)
	fakeBinDir := writeFakeTool(t, "az", fakeToolConfig{})
	configPath := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(configPath, []byte("[[containers]]\ntype = \"azure\"\ntag = \"latest\"\nport = \"4566\"\n"), 0o644))
	environ := env.Environ(testEnvWithHome(t.TempDir(), "")).
		With(env.AnalyticsEndpoint, analyticsSrv.URL).
		With(env.Path, fakeBinDir)
	environ = append(environ, unreachableDockerHost)

	_, _, err := runLstk(t, testContext(t), t.TempDir(), environ, "--config", configPath, "setup", "azure", "--non-interactive")
	require.Error(t, err)

	params, result := commandEventParts(t, receiveEventByName(t, events, "lstk_command"))
	assert.Equal(t, "setup azure", params["command"])
	assert.Equal(t, false, params["proxied"])
	assert.Equal(t, "EMULATOR_NOT_RUNNING", result["error_code"])
	assert.Equal(t, "EMULATOR", result["error_category"])
}

// lstk's own `aws s3api create-bucket` failing while provisioning a terraform
// S3 backend propagates the child's exit code (Decision 6) and must carry a
// code, or it reads as an unclassified lstk failure with an odd exit code.
func TestTerraformBackendProvisioningFailureTelemetryIsClassified(t *testing.T) {
	requireTerraform(t)
	t.Parallel()

	emulatorSrv := awsHealthServer(t)
	defer emulatorSrv.Close()
	analyticsSrv, events := mockAnalyticsServer(t)
	fakeBinDir := writeFakeTool(t, "aws", fakeToolConfig{Stderr: []string{"An error occurred (fake) when calling CreateBucket"}, ExitCode: 5})

	work := t.TempDir()
	require.NoError(t, os.WriteFile(work+"/main.tf", []byte("terraform {\n  backend \"s3\" {\n    bucket = \"lstk-tfstate-test\"\n    key    = \"state\"\n    region = \"us-east-1\"\n  }\n}\n"), 0o644))

	_, _, err := runLstk(t, testContext(t), work, proxyEnviron(t, analyticsSrv.URL, fakeBinDir),
		"--endpoint-url", emulatorSrv.URL, "terraform", "init", "-input=false")
	require.Error(t, err)
	requireExitCode(t, 5, err)

	params, result := commandEventParts(t, receiveEventByName(t, events, "lstk_command"))
	assert.Equal(t, true, params["proxied"])
	assert.NotContains(t, result, "proxy_exit_code")
	assert.InDelta(t, 5, result["exit_code"], 0)
	assert.Equal(t, "IAC_DEPLOY_FAILED", result["error_code"])
	assert.Equal(t, "IAC", result["error_category"])
}

// Two of the previously unclassified sites, one per carrier shape: a preflight
// that emits and returns (INTEGRATION_NOT_SET_UP) and a platform-side
// precondition (AUTH_REQUIRED).
func TestAzWithoutSetupTelemetryIsClassified(t *testing.T) {
	t.Parallel()

	emulatorSrv := azureHealthServer(t)
	analyticsSrv, events := mockAnalyticsServer(t)
	fakeBinDir := writeFakeTool(t, "az", fakeToolConfig{})
	configPath := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(configPath, []byte("[[containers]]\ntype = \"azure\"\ntag = \"latest\"\nport = \"4566\"\n"), 0o644))
	environ := env.Environ(testEnvWithHome(t.TempDir(), "")).
		With(env.AnalyticsEndpoint, analyticsSrv.URL).
		With(env.Path, fakeBinDir)
	environ = append(environ, unreachableDockerHost)

	_, _, err := runLstk(t, testContext(t), t.TempDir(), environ,
		"--endpoint-url", emulatorSrv.URL, "--config", configPath, "--non-interactive", "az", "group", "list")
	require.Error(t, err)

	params, result := commandEventParts(t, receiveEventByName(t, events, "lstk_command"))
	assert.Equal(t, true, params["proxied"])
	assert.NotContains(t, result, "proxy_exit_code")
	assert.Equal(t, "INTEGRATION_NOT_SET_UP", result["error_code"])
	assert.Equal(t, "CONFIG", result["error_category"])
}

func TestSnapshotShowWithoutAuthTelemetryIsClassified(t *testing.T) {
	t.Parallel()

	analyticsSrv, events := mockAnalyticsServer(t)
	environ := env.Environ(testEnvWithHome(t.TempDir(), "")).
		With(env.AnalyticsEndpoint, analyticsSrv.URL).
		Without(env.AuthToken)

	_, _, err := runLstk(t, testContext(t), "", environ, "snapshot", "show", "pod:does-not-exist")
	require.Error(t, err)

	_, result := commandEventParts(t, receiveEventByName(t, events, "lstk_command"))
	assert.Equal(t, "AUTH_REQUIRED", result["error_code"])
	assert.Equal(t, "AUTH", result["error_category"])
}

// receiveEventByName waits up to 3s for an event with the given name.
// Events with a different name are skipped until the deadline.
func receiveEventByName(t *testing.T, events <-chan map[string]any, name string) map[string]any {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case event := <-events:
			if event["name"] == name {
				return event
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %q telemetry event", name)
			return nil
		}
	}
}

// asserts that a lstk_command event was emitted with the expected command name and exit code
func assertCommandTelemetry(t *testing.T, events <-chan map[string]any, command string, exitCode int) {
	t.Helper()
	event := receiveEventByName(t, events, "lstk_command")
	payload, _ := event["payload"].(map[string]any)
	params, _ := payload["parameters"].(map[string]any)
	assert.Equal(t, command, params["command"])
	result, _ := payload["result"].(map[string]any)
	assert.InDelta(t, exitCode, result["exit_code"], 0)
}

// Regression test for FLC-648: a slow analytics endpoint must not add to
// command latency since the flush happens in a detached subprocess.
func TestStartCommand_DoesNotBlockOnSlowAnalyticsEndpoint(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	const endpointDelay = 3 * time.Second
	const maxParentDuration = endpointDelay / 3 // generous: anything <1s proves the point

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	startTestContainer(t, ctx)

	received := make(chan map[string]any, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Events []map[string]any `json:"events"`
		}
		if err := json.Unmarshal(body, &req); err == nil {
			for _, ev := range req.Events {
				received <- ev
			}
		}
		time.Sleep(endpointDelay)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	cmd := exec.CommandContext(ctx, binaryPath(), "start")
	cmd.Env = env.Environ(testEnvWithHome(t.TempDir(), "")).
		With(env.AuthToken, "fake-token").
		With(env.AnalyticsEndpoint, srv.URL)

	start := time.Now()
	out, err := cmd.CombinedOutput()
	parentDuration := time.Since(start)

	require.NoError(t, err, "lstk start failed: %s", out)
	require.Less(t, parentDuration, maxParentDuration,
		"parent process took %v, expected <%v — subprocess flush should not block parent", parentDuration, maxParentDuration)

	// The detached subprocess should still deliver the event.
	select {
	case <-received:
	case <-time.After(endpointDelay + 5*time.Second):
		t.Fatal("subprocess flusher never delivered the telemetry event")
	}
}

// Verifies the detached flush path without Docker, so it also covers the
// platform-specific detach flags on Windows CI.
func TestCommandTelemetryIsDeliveredByDetachedFlusher(t *testing.T) {
	t.Parallel()

	ctx := testContext(t)
	analyticsSrv, events := mockAnalyticsServer(t)

	// Dead DOCKER_HOST: start fails fast without Docker but still emits telemetry.
	_, _, err := runLstk(t, ctx, "", env.Environ(testEnvWithHome(t.TempDir(), "")).
		With(env.AuthToken, "fake-token").
		With(env.AnalyticsEndpoint, analyticsSrv.URL).
		With(env.Key("DOCKER_HOST"), "tcp://127.0.0.1:1"), "start")
	require.Error(t, err)
	requireExitCode(t, 1, err)

	assertCommandTelemetry(t, events, "start", 1)
}

// Pipes events into the hidden __flush-telemetry subcommand and verifies they
// are POSTed without the subcommand emitting telemetry about itself.
func TestFlushTelemetrySubcommandDoesNotSpawnRecursively(t *testing.T) {
	t.Parallel()

	ctx := testContext(t)
	analyticsSrv, events := mockAnalyticsServer(t)

	input := `{"name":"first_event","metadata":{"client_time":"2026-06-02 00:00:00.000000","session_id":"s1"},"payload":{"k":"v1"}}
{"name":"second_event","metadata":{"client_time":"2026-06-02 00:00:00.000000","session_id":"s1"},"payload":{"k":"v2"}}
`

	cmd := exec.CommandContext(ctx, binaryPath(), "__flush-telemetry", "--endpoint", analyticsSrv.URL)
	// Route the subprocess's own telemetry at the mock too, so a recursive event would be observable.
	cmd.Env = env.Environ(testEnvWithHome(t.TempDir(), "")).
		With(env.AnalyticsEndpoint, analyticsSrv.URL)
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "__flush-telemetry failed: %s", out)

	got := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case ev := <-events:
			got[ev["name"].(string)] = true
		default:
			t.Fatalf("expected 2 flushed events, got %d: %v", i, got)
		}
	}
	assert.True(t, got["first_event"] && got["second_event"], "expected both piped events, got: %v", got)

	// No further events may arrive: that would mean the flusher emitted telemetry about itself.
	select {
	case ev := <-events:
		t.Fatalf("unexpected extra telemetry event from flusher: %v", ev)
	case <-time.After(2 * time.Second):
	}
}

// Verifies the detached flusher exports its spans to the OTLP collector via the
// propagated TRACEPARENT; trace-ID equality is covered in internal/tracing.
func TestOtelFlushSpansJoinCommandTrace(t *testing.T) {
	t.Parallel()

	ctx := testContext(t)
	analyticsSrv, _ := mockAnalyticsServer(t)
	otlpSrv, otlpBodies := mockOTLPCollector(t)

	// Dead DOCKER_HOST: start fails fast without Docker but still emits telemetry.
	_, _, err := runLstk(t, ctx, "", env.Environ(testEnvWithHome(t.TempDir(), "")).
		With(env.AuthToken, "fake-token").
		With(env.AnalyticsEndpoint, analyticsSrv.URL).
		With(env.Otel, "1").
		With(env.OtelEndpoint, otlpSrv.URL).
		With(env.Key("DOCKER_HOST"), "tcp://127.0.0.1:1"), "start")
	require.Error(t, err)
	requireExitCode(t, 1, err)

	// The flusher subprocess exports after the parent exits; allow for its
	// startup, the POST, and the batcher flush on shutdown.
	deadline := time.After(10 * time.Second)
	for {
		select {
		case body := <-otlpBodies:
			// otlptracehttp serializes spans as protobuf; UTF-8 strings appear
			// inline in the wire format, so a substring search is sufficient.
			if bytes.Contains(body, []byte("telemetry flush")) {
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for flush span in OTLP export — TRACEPARENT propagation or subprocess tracing broken")
		}
	}
}

// mockOTLPCollector returns a test server that accepts OTLP/HTTP trace exports
// and forwards each (decompressed) request body to the returned channel.
func mockOTLPCollector(t *testing.T) (*httptest.Server, <-chan []byte) {
	t.Helper()
	bodies := make(chan []byte, 16)
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reader io.Reader = r.Body
		if r.Header.Get("Content-Encoding") == "gzip" {
			gz, err := gzip.NewReader(r.Body)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			defer func() { _ = gz.Close() }()
			reader = gz
		}
		body, err := io.ReadAll(reader)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		select {
		case bodies <- body:
		default:
			once.Do(func() { t.Logf("OTLP body channel full, dropping payload of %d bytes", len(body)) })
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv, bodies
}

// collects events until count distinct event names have been received or the deadline expires.
func collectTelemetryByName(t *testing.T, events <-chan map[string]any, count int) map[string]map[string]any {
	t.Helper()
	byName := make(map[string]map[string]any)
	deadline := time.After(3 * time.Second)
	for len(byName) < count {
		select {
		case event := <-events:
			name, _ := event["name"].(string)
			byName[name] = event
		case <-deadline:
			t.Fatalf("timed out waiting for %d telemetry events; received: %v", count, byName)
		}
	}
	return byName
}
