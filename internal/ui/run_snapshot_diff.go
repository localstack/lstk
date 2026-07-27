package ui

import (
	"context"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/snapshot"
)

func RunSnapshotDiff(parentCtx context.Context, rt runtime.Runtime, containers []config.ContainerConfig, client snapshot.PodDiffer, host, podName, authToken, strategy string) error {
	return runWithTUI(parentCtx, withoutHeader(), func(ctx context.Context, sink output.Sink) error {
		return snapshot.DiffPod(ctx, rt, containers, client, host, podName, authToken, strategy, sink)
	})
}
