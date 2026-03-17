package container

import (
	"context"
	"fmt"
	"time"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
)

func Stop(ctx context.Context, rt runtime.Runtime, sink output.Sink, containers []config.ContainerConfig) error {
	const stopTimeout = 30 * time.Second

	for _, c := range containers {
		name := c.Name()

		checkCtx, checkCancel := context.WithTimeout(ctx, 5*time.Second)
		defer checkCancel()
		running, err := rt.IsRunning(checkCtx, name)
		if err != nil {
			return fmt.Errorf("checking %s running: %w", name, err)
		}
		if !running {
			return fmt.Errorf("LocalStack is not running")
		}
		output.EmitSpinnerStart(sink, "Stopping LocalStack...")
		stopCtx, stopCancel := context.WithTimeout(ctx, stopTimeout)
		defer stopCancel()
		if err := rt.Stop(stopCtx, name); err != nil {
			output.EmitSpinnerStop(sink)
			return fmt.Errorf("failed to stop LocalStack: %w", err)
		}
		output.EmitSpinnerStop(sink)
		output.EmitSuccess(sink, "LocalStack stopped")
	}

	return nil
}
