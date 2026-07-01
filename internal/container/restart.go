package container

import (
	"context"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
)

func Restart(ctx context.Context, rt runtime.Runtime, sink output.Sink, stopOpts StopOptions, startOpts StartOptions, interactive bool) error {
	// Persistence is a property of the running instance, not just a per-run flag.
	// If the caller didn't explicitly request it, carry forward the setting of the
	// currently running container so `lstk restart` doesn't silently drop persistence.
	if !startOpts.Persist {
		startOpts.Persist = runningPersistenceEnabled(ctx, rt, startOpts.Containers)
	}

	if err := Stop(ctx, rt, sink, startOpts.Containers, stopOpts); err != nil {
		return err
	}
	_, err := Start(ctx, rt, sink, startOpts, interactive)
	return err
}

// runningPersistenceEnabled reports whether any of the running containers was
// started with persistence enabled.
func runningPersistenceEnabled(ctx context.Context, rt runtime.Runtime, containers []config.ContainerConfig) bool {
	for _, c := range containers {
		name, err := ResolveRunningContainerName(ctx, rt, c)
		if err != nil || name == "" {
			continue
		}
		if isPersistenceEnabled(ctx, rt, name) {
			return true
		}
	}
	return false
}
