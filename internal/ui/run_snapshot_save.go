package ui

import (
	"context"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/snapshot"
)

func RunSnapshotSaveLocal(parentCtx context.Context, rt runtime.Runtime, containers []config.ContainerConfig, exporter snapshot.StateExporter, host, dest string) error {
	return runWithTUI(parentCtx, withoutHeader(), func(ctx context.Context, sink output.Sink) error {
		return snapshot.SaveLocal(ctx, rt, containers, exporter, host, dest, sink)
	})
}

func RunSnapshotSavePod(parentCtx context.Context, rt runtime.Runtime, containers []config.ContainerConfig, saver snapshot.PodSaver, host, podName, authToken string) error {
	return runWithTUI(parentCtx, withoutHeader(), func(ctx context.Context, sink output.Sink) error {
		return snapshot.SavePod(ctx, rt, containers, saver, host, podName, authToken, sink)
	})
}
