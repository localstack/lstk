package container

import (
	"context"
	"fmt"
	"time"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
)

const stopTimeout = 30 * time.Second

func Stop(ctx context.Context, rt runtime.Runtime, sink output.Sink, containers []config.ContainerConfig) error {
	for _, c := range containers {
		name := c.Name()

		containerCtx, containerCancel := context.WithTimeout(ctx, stopTimeout)
		running, err := rt.IsRunning(containerCtx, name)
		if err != nil {
			containerCancel()
			return fmt.Errorf("checking %s running: %w", name, err)
		}
		if !running {
			containerCancel()
			return fmt.Errorf("LocalStack is not running")
		}
		output.EmitSpinnerStart(sink, "Stopping LocalStack...")
		if err := rt.Stop(containerCtx, name); err != nil {
			output.EmitSpinnerStop(sink)
			containerCancel()
			return fmt.Errorf("failed to stop LocalStack: %w", err)
		}
		output.EmitSpinnerStop(sink)
		output.EmitSuccess(sink, "LocalStack stopped")
		containerCancel()
	}

	return nil
}
