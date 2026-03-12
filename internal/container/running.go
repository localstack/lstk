package container

import (
	"context"
	"fmt"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/runtime"
)

func AnyRunning(ctx context.Context, rt runtime.Runtime, containers []config.ContainerConfig) (bool, error) {
	for _, c := range containers {
		running, err := rt.IsRunning(ctx, c.Name())
		if err != nil {
			return false, fmt.Errorf("checking %s running: %w", c.Name(), err)
		}
		if running {
			return true, nil
		}
	}

	return false, nil
}
