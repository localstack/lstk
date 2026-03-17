package container

import (
	"context"
	"fmt"
	"time"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
)

func Stop(ctx context.Context, rt runtime.Runtime, sink output.Sink, containers []config.ContainerConfig, timeout time.Duration) error {
	for _, c := range containers {
		name := c.Name()

		checkCtx, checkCancel := context.WithTimeout(ctx, 5*time.Second)
		running, err := rt.IsRunning(checkCtx, name)
		checkCancel()
		if err != nil {
			return fmt.Errorf("checking %s running: %w", name, err)
		}
		if !running {
			return fmt.Errorf("LocalStack is not running")
		}
		output.EmitSpinnerStart(sink, "Stopping LocalStack...")
		stopCtx, stopCancel := context.WithTimeout(ctx, timeout)
		if err := rt.Stop(stopCtx, name); err != nil {
			output.EmitSpinnerStop(sink)
			stopCancel()
			return fmt.Errorf("failed to stop LocalStack: %w", err)
		}
		stopCancel()
		output.EmitSpinnerStop(sink)
		output.EmitSuccess(sink, "LocalStack stopped")
	}

	return nil
}
