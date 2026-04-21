package container

import (
	"context"
	"fmt"
	"strings"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/runtime"
)

func AnyRunning(ctx context.Context, rt runtime.Runtime, containers []config.ContainerConfig) (bool, error) {
	for _, c := range containers {
		name, err := resolveRunningContainerName(ctx, rt, c)
		if err != nil {
			return false, err
		}
		if name != "" {
			return true, nil
		}
	}

	return false, nil
}

// First checks the configured name,
// then falls back to FindRunningByImage for containers started outside lstk
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

	found, err := rt.FindRunningByImage(ctx, imageRepo, containerPort)
	if err != nil {
		return "", fmt.Errorf("failed to scan for running containers: %w", err)
	}
	if found != nil {
		return found.Name, nil
	}

	return "", nil
}
