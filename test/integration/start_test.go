package integration_test

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const containerName = "localstack-aws"

func TestStartCommandFailsWithoutAuth(t *testing.T) {
	cleanup()
	t.Cleanup(cleanup)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "../../bin/lstk", "start")
	output, err := cmd.CombinedOutput()

	require.Error(t, err, "expected lstk start to fail without auth")
	assert.Contains(t, string(output), "License activation failed")
}

func cleanup() {
	ctx := context.Background()
	dockerClient.ContainerStop(ctx, containerName, container.StopOptions{})
	dockerClient.ContainerRemove(ctx, containerName, container.RemoveOptions{Force: true})
}
