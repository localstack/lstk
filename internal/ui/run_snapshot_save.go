package ui

import (
	"context"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/snapshot"
)

func RunSnapshotSave(parentCtx context.Context, rt runtime.Runtime, containers []config.ContainerConfig, exporter snapshot.StateExporter, dest snapshot.Destination) error {
	return runWithTUI(parentCtx, withoutHeader(), func(ctx context.Context, sink output.Sink) error {
		return snapshot.Save(ctx, rt, containers, exporter, dest, sink)
	})
}
