package container

import (
	"context"
	"fmt"
	"time"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/telemetry"
)

// StopOptions carries optional telemetry context for the stop command.
type StopOptions struct {
	Telemetry *telemetry.Client
}

func Stop(ctx context.Context, rt runtime.Runtime, sink output.Sink, containers []config.ContainerConfig, opts StopOptions) error {
	if err := rt.IsHealthy(ctx); err != nil {
		rt.EmitUnhealthyError(sink, err)
		return output.NewSilentError(fmt.Errorf("runtime not healthy: %w", err))
	}

	const stopTimeout = 30 * time.Second
	for _, c := range containers {
		name, err := resolveRunningContainerName(ctx, rt, c)
		if err != nil {
			return err
		}
		if name == "" {
			return fmt.Errorf("LocalStack is not running")
		}

		// Fetch localstack info before stopping so it can be included in telemetry.
		lsInfo, _ := fetchLocalStackInfo(ctx, c.Port)

		stopStart := time.Now()

		output.EmitSpinnerStart(sink, "Stopping LocalStack...")
		stopCtx, stopCancel := context.WithTimeout(ctx, stopTimeout)
		if err := rt.Stop(stopCtx, name); err != nil {
			stopCancel()
			output.EmitSpinnerStop(sink)
			return fmt.Errorf("failed to stop LocalStack: %w", err)
		}
		stopCancel()
		output.EmitSpinnerStop(sink)
		output.EmitSuccess(sink, "LocalStack stopped")

		if opts.Telemetry != nil {
			opts.Telemetry.EmitEmulatorLifecycleEvent(ctx, telemetry.LifecycleEvent{
				EventType:      telemetry.LifecycleStop,
				Emulator:       string(c.Type),
				DurationMS:     time.Since(stopStart).Milliseconds(),
				LocalStackInfo: lsInfo,
			})
		}
	}

	return nil
}
