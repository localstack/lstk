package container

import (
	"context"
	"fmt"
	"strings"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/telemetry"
)

func StillRunningMessage(running []config.ContainerConfig) string {
	names := make([]string, len(running))
	for i, c := range running {
		names[i] = c.DisplayName()
	}
	if len(names) == 1 {
		return fmt.Sprintf("%s is still running in the background", names[0])
	}
	return fmt.Sprintf("%s are still running in the background", strings.Join(names, ", "))
}

func RunningEmulators(ctx context.Context, rt runtime.Runtime, containers []config.ContainerConfig) ([]config.ContainerConfig, error) {
	var running []config.ContainerConfig
	for _, c := range containers {
		name, err := ResolveRunningContainerName(ctx, rt, c)
		if err != nil {
			return nil, err
		}
		if name != "" {
			running = append(running, c)
		}
	}
	return running, nil
}

func ResolveRunningContainerName(ctx context.Context, rt runtime.Runtime, c config.ContainerConfig) (string, error) {
	running, err := rt.IsRunning(ctx, c.Name())
	if err != nil {
		return "", fmt.Errorf("checking %s running: %w", c.Name(), err)
	}
	if running {
		return c.Name(), nil
	}

	containerPort, err := c.ContainerPort()
	if err != nil {
		return "", err
	}

	found, err := rt.FindRunningByImage(ctx, config.KnownImageReposForType(c.Type), containerPort)
	if err != nil {
		return "", fmt.Errorf("failed to scan for running containers: %w", err)
	}
	if found != nil {
		return found.Name, nil
	}

	return "", nil
}

// ResolvedEmulator reports how the emulator can be reached.
type ResolvedEmulator struct {
	ContainerName string                    // non-empty: managed container found via the runtime
	External      bool                      // reachable over HTTP only (e.g. running from source)
	Info          *telemetry.LocalStackInfo // /_localstack/info payload when External
}

func (r ResolvedEmulator) Found() bool {
	return r.ContainerName != "" || r.External
}

// ResolveEmulator locates the emulator described by c: first via the container
// runtime (skipped when rt is nil, i.e. Docker is unavailable), then by probing
// host's /_localstack/info endpoint, which also finds instances lstk did not
// start (e.g. LocalStack running from source). It emits nothing; the returned
// error covers only runtime-API failures — an unreachable endpoint is a
// not-found result, not an error.
func ResolveEmulator(ctx context.Context, rt runtime.Runtime, c config.ContainerConfig, host string) (ResolvedEmulator, error) {
	if rt != nil {
		name, err := ResolveRunningContainerName(ctx, rt, c)
		if err != nil {
			return ResolvedEmulator{}, err
		}
		if name != "" {
			return ResolvedEmulator{ContainerName: name}, nil
		}
	}

	info, err := ProbeEmulatorInfo(ctx, host)
	if err != nil {
		return ResolvedEmulator{}, nil
	}

	// The probe cannot tell which emulator product answered. When a known
	// LocalStack container of any type is running, the answer is that
	// container, not an external instance — report not-found so callers keep
	// today's type-mismatch errors. Without Docker the guard cannot run; the
	// looseness is accepted (from-source runs are overwhelmingly single-type).
	if rt != nil {
		containerPort, err := c.ContainerPort()
		if err != nil {
			return ResolvedEmulator{}, err
		}
		found, err := rt.FindRunningByImage(ctx, config.KnownImageRepos(), containerPort)
		if err != nil {
			return ResolvedEmulator{}, fmt.Errorf("failed to scan for running containers: %w", err)
		}
		if found != nil {
			return ResolvedEmulator{}, nil
		}
	}

	return ResolvedEmulator{External: true, Info: info}, nil
}

// FirstReachableEmulator returns the first container from containers that is
// reachable: via Docker discovery when the runtime is healthy, else via the
// HTTP probe of host (which also finds instances lstk did not start, e.g.
// LocalStack running from source). Docker being unhealthy is fatal only when
// the probe finds nothing either — then the standard unhealthy error is
// emitted through sink and a silent error returned, preserving today's
// behavior. A zero result with a nil error means Docker is healthy but
// nothing answered; callers emit their own not-running message.
func FirstReachableEmulator(ctx context.Context, rt runtime.Runtime, sink output.Sink, containers []config.ContainerConfig, host string) (config.ContainerConfig, ResolvedEmulator, error) {
	discoveryRT := rt
	healthErr := rt.IsHealthy(ctx)
	if healthErr != nil {
		discoveryRT = nil
	}

	for _, c := range containers {
		resolved, err := ResolveEmulator(ctx, discoveryRT, c, host)
		if err != nil {
			return config.ContainerConfig{}, ResolvedEmulator{}, fmt.Errorf("checking emulator status: %w", err)
		}
		if resolved.Found() {
			return c, resolved, nil
		}
	}

	if healthErr != nil {
		rt.EmitUnhealthyError(sink, healthErr)
		return config.ContainerConfig{}, ResolvedEmulator{}, output.NewSilentError(fmt.Errorf("runtime not healthy: %w", healthErr))
	}
	return config.ContainerConfig{}, ResolvedEmulator{}, nil
}

// HandleNoRunningContainer emits the standard "not running" error for c
// through sink (naming it and pointing the user at how to start it), then
// returns a silent error for the caller to propagate. Callers that already
// resolved ResolveRunningContainerName to "" should use this instead of
// duplicating the ErrorEvent/Actions boilerplate.
func HandleNoRunningContainer(sink output.Sink, c config.ContainerConfig) error {
	sink.Emit(output.ErrorEvent{
		Title: fmt.Sprintf("%s is not running", c.DisplayName()),
		Actions: []output.ErrorAction{
			{Label: "Start LocalStack:", Value: "lstk"},
			{Label: "See help:", Value: "lstk -h"},
		},
	})
	return output.NewSilentError(fmt.Errorf("%s is not running", c.Name()))
}
