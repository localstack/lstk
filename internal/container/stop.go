package container

import (
	"context"
	"fmt"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
)

func Stop(ctx context.Context, rt runtime.Runtime, sink output.Sink, containers []config.ContainerConfig) error {
	for _, c := range containers {
		name := c.Name()
		running, err := rt.IsRunning(ctx, name)
		if err != nil {
			return fmt.Errorf("checking %s running: %w", name, err)
		}
		if !running {
			return fmt.Errorf("LocalStack is not running")
		}
		output.EmitSpinnerStart(sink, "Stopping LocalStack...")
		if err := rt.Stop(ctx, name); err != nil {
			output.EmitSpinnerStop(sink)
			return fmt.Errorf("failed to stop LocalStack: %w", err)
		}
		output.EmitSpinnerStop(sink)
		output.EmitSuccess(sink, "LocalStack stopped")
	}

	return nil
}
