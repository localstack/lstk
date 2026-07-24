package cmd

// Command-boundary emulator reachability shared by the proxy commands (aws,
// az, terraform, cdk, sam). Lives in cmd/ (not a domain package) because it
// constructs the runtime from env config and renders errors through the sink.

import (
	"context"
	"fmt"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/container"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
)

// resolveReachableEmulator constructs the Docker runtime, checks its health,
// and resolves the emulator via container.ResolveEmulator: Docker discovery
// first, then an HTTP probe of host, which also finds instances lstk did not
// start (e.g. LocalStack running from source). Docker being unavailable is
// fatal only when the probe finds nothing either, preserving today's errors
// (raw construction error, or the standard unhealthy ErrorEvent as a silent
// error). err == nil with !resolved.Found() means Docker is healthy but
// nothing answered — the caller picks its own not-running message. The
// returned runtime is non-nil only when Docker is healthy.
func resolveReachableEmulator(ctx context.Context, dockerHost string, sink output.Sink, c config.ContainerConfig, host string) (container.ResolvedEmulator, runtime.Runtime, error) {
	var healthyRT runtime.Runtime
	var healthErr error
	rt, rtErr := runtime.NewDockerRuntime(dockerHost)
	if rtErr == nil {
		if healthErr = rt.IsHealthy(ctx); healthErr == nil {
			healthyRT = rt
		}
	}

	resolved, err := container.ResolveEmulator(ctx, healthyRT, c, host)
	if err != nil {
		return container.ResolvedEmulator{}, healthyRT, fmt.Errorf("checking emulator status: %w", err)
	}
	if resolved.Found() {
		return resolved, healthyRT, nil
	}
	if rtErr != nil {
		return container.ResolvedEmulator{}, nil, rtErr
	}
	if healthErr != nil {
		rt.EmitUnhealthyError(sink, healthErr)
		return container.ResolvedEmulator{}, nil, output.NewSilentError(fmt.Errorf("runtime not healthy: %w", healthErr))
	}
	return container.ResolvedEmulator{}, healthyRT, nil
}
