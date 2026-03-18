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
	AuthToken string
}

func Stop(ctx context.Context, rt runtime.Runtime, sink output.Sink, containers []config.ContainerConfig, opts StopOptions) error {
	const stopTimeout = 30 * time.Second
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
			opts.Telemetry.Emit(ctx, "lstk_lifecycle", telemetry.ToMap(telemetry.LifecycleEvent{
				EventType:      telemetry.LifecycleStop,
				Environment:    opts.Telemetry.GetEnvironment(opts.AuthToken),
				Emulator:       string(c.Type),
				DurationMS:     time.Since(stopStart).Milliseconds(),
				LocalStackInfo: lsInfo,
			}))
		}
	}

	return nil
}
