package integration_test

import (
	"bufio"
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogsCommandFailsWhenNotRunning(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath(), "logs")
	out, err := cmd.CombinedOutput()

	require.Error(t, err, "expected lstk logs to fail when container not running")
	assert.Contains(t, string(out), "emulator is not running")
}

func TestLogsCommandStreamsOutput(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	startTestContainer(t, ctx)

	const marker = "lstk-logs-test-marker"

	logsCmd := exec.CommandContext(ctx, binaryPath(), "logs")
	stdout, err := logsCmd.StdoutPipe()
	require.NoError(t, err, "failed to get stdout pipe")

	err = logsCmd.Start()
	require.NoError(t, err, "failed to start lstk logs")
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
		t.Fatal("marker did not appear in lstk logs output within timeout")
	}
}
