package container

import (
	"context"
	"fmt"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
)

func Stop(ctx context.Context, rt runtime.Runtime, sink output.Sink) error {
	cfg, err := config.Get()
	if err != nil {
		return fmt.Errorf("failed to get config: %w", err)
	}

	for _, c := range cfg.Containers {
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
