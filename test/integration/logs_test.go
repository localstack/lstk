package integration_test

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeAwsConfig writes a minimal aws-only config so tests don't inherit the
// developer's real ~/.config/lstk/config.toml (which may target a different
// emulator / running container).
func writeAwsConfig(t *testing.T) string {
	t.Helper()
	configFile := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(configFile, []byte(`
[[containers]]
type = "aws"
tag  = "latest"
port = "4566"
`), 0644))
	return configFile
}

func TestLogsExitsByDefault(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)

	configFile := writeAwsConfig(t)
	analyticsSrv, events := mockAnalyticsServer(t)
	_, _, err := runLstk(t, ctx, "", env.With(env.AnalyticsEndpoint, analyticsSrv.URL), "--config", configFile, "logs")
	require.NoError(t, err, "lstk logs should exit cleanly when container is running")
	requireExitCode(t, 0, err)
	assertCommandTelemetry(t, events, "logs", 0)
}

func TestLogsCommandFailsWhenNotRunning(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	configFile := writeAwsConfig(t)
	analyticsSrv, events := mockAnalyticsServer(t)
	_, stderr, err := runLstk(t, testContext(t), "", env.With(env.AnalyticsEndpoint, analyticsSrv.URL), "--config", configFile, "logs", "--follow")
	require.Error(t, err, "expected lstk logs --follow to fail when container not running")
	requireExitCode(t, 1, err)
	assert.Contains(t, stderr, "emulator is not running")
	assertCommandTelemetry(t, events, "logs", 1)
}

func TestLogsFollowStreamsOutput(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)

	const marker = "lstk-logs-test-marker"

	configFile := writeAwsConfig(t)
	// Uses StdoutPipe for streaming — cannot use runLstk.
	logsCmd := exec.CommandContext(ctx, binaryPath(), "--config", configFile, "logs", "--follow")
	stdout, err := logsCmd.StdoutPipe()
	require.NoError(t, err, "failed to get stdout pipe")

	err = logsCmd.Start()
	require.NoError(t, err, "failed to start lstk logs --follow")
	t.Cleanup(func() { _ = logsCmd.Process.Kill() })

	// Give lstk logs a moment to connect before generating output
	time.Sleep(500 * time.Millisecond)

	// Write to /proc/1/fd/1 (PID 1's stdout) so the line appears in docker logs.
	execResp, err := dockerClient.ContainerExecCreate(ctx, containerName, container.ExecOptions{
		Cmd: []string{"sh", "-c", "echo " + marker + " >/proc/1/fd/1"},
	})
	require.NoError(t, err, "failed to create exec")
	err = dockerClient.ContainerExecStart(ctx, execResp.ID, container.ExecStartOptions{Detach: true})
	require.NoError(t, err, "failed to start exec")

	// Scan lines in a goroutine because reading from the pipe blocks until lstk logs exits, which it never does on its own.
	found := make(chan struct{}, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if strings.Contains(scanner.Text(), marker) {
				found <- struct{}{}
				return
			}
		}
	}()

	select {
	case <-found:
	case <-ctx.Done():
		t.Fatal("marker did not appear in lstk logs --follow output within timeout")
	}
}
