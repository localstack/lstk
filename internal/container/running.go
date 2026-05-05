package container

import (
	"context"
	"fmt"
	"strings"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/runtime"
)

func StillRunningMessage(running []config.ContainerConfig) string {
	names := make([]string, len(running))
	for i, c := range running {
		names[i] = c.DisplayName()
	}
	if len(names) == 1 {
		return fmt.Sprintf("%s is still running in the background", names[0])
	}
	return fmt.Sprintf("%s are still running in the background", strings.Join(names, ", "))
}

func RunningEmulators(ctx context.Context, rt runtime.Runtime, containers []config.ContainerConfig) ([]config.ContainerConfig, error) {
	var running []config.ContainerConfig
	for _, c := range containers {
		name, err := resolveRunningContainerName(ctx, rt, c)
		if err != nil {
			return nil, err
		}
		if name != "" {
			running = append(running, c)
		}
	}
	return running, nil
}

func resolveRunningContainerName(ctx context.Context, rt runtime.Runtime, c config.ContainerConfig) (string, error) {
	running, err := rt.IsRunning(ctx, c.Name())
	if err != nil {
		return "", fmt.Errorf("checking %s running: %w", c.Name(), err)
	}
	if running {
		return c.Name(), nil
	}

	image, err := c.Image()
	if err != nil {
		return "", err
	}
	imageRepo, _, _ := strings.Cut(image, ":")

	containerPort, err := c.ContainerPort()
	if err != nil {
		return "", err
	}

	found, err := rt.FindRunningByImage(ctx, []string{imageRepo, "localstack/localstack"}, containerPort)
	if err != nil {
		return "", fmt.Errorf("failed to scan for running containers: %w", err)
	}
	if found != nil {
		return found.Name, nil
	}

	return "", nil
}
