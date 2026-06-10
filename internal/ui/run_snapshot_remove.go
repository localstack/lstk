package ui

import (
	"context"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/snapshot"
)

func RunSnapshotRemove(parentCtx context.Context, rt runtime.Runtime, containers []config.ContainerConfig, client snapshot.PodRemover, host, ref, cwd, home, authToken string, force bool) error {
	return runWithTUI(parentCtx, withoutHeader(), func(ctx context.Context, sink output.Sink) error {
		dst, err := snapshot.ParseRemovable(ref, cwd, home)
		if err != nil {
			return err
		}
		return snapshot.Remove(ctx, rt, containers, dst.Value, authToken, client, host, force, sink)
	})
}
