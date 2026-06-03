package ui

import (
	"context"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/snapshot"
)

func RunSnapshotRemove(parentCtx context.Context, rt runtime.Runtime, containers []config.ContainerConfig, client snapshot.PodRemover, host, podName, authToken string, force bool) error {
	return runWithTUI(parentCtx, withoutHeader(), func(ctx context.Context, sink output.Sink) error {
		return snapshot.Remove(ctx, rt, containers, podName, authToken, client, host, force, sink)
	})
}
