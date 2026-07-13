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
		name, err := ResolveRunningContainerName(ctx, rt, c)
		if err != nil {
			return err
		}
		if name == "" {
			sink.Emit(output.ErrorEvent{
				Title: fmt.Sprintf("%s is not running", c.DisplayName()),
				Code:  output.ErrEmulatorNotRunning,
			})
			return output.NewSilentError(fmt.Errorf("%s is not running", c.Name()))
		}

		// Fetch localstack info before stopping so it can be included in telemetry.
		lsInfo, _ := fetchLocalStackInfo(ctx, c.Port)

		stopStart := time.Now()

		sink.Emit(output.SpinnerStart("Stopping LocalStack..."))
		stopCtx, stopCancel := context.WithTimeout(ctx, stopTimeout)
		if err := rt.Stop(stopCtx, name); err != nil {
			stopCancel()
			sink.Emit(output.SpinnerStop())
			wrapped := fmt.Errorf("failed to stop LocalStack: %w", err)
			sink.Emit(output.ErrorEvent{Title: wrapped.Error(), Code: output.ErrRuntimeUnavailable})
			return output.NewSilentError(wrapped)
		}
		stopCancel()
		sink.Emit(output.SpinnerStop())
		sink.Emit(output.EmulatorStoppedEvent{Type: string(c.Type), Name: name, DisplayName: c.DisplayName(), WasRunning: true})

		opts.Telemetry.EmitEmulatorLifecycleEvent(ctx, telemetry.LifecycleEvent{
			EventType:      telemetry.LifecycleStop,
			Emulator:       c.Type,
			DurationMS:     time.Since(stopStart).Milliseconds(),
			LocalStackInfo: lsInfo,
		})
	}

	return nil
}
