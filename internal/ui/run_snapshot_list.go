package ui

import (
	"context"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/snapshot"
)

func RunSnapshotList(parentCtx context.Context, rt runtime.Runtime, containers []config.ContainerConfig, lister snapshot.PodLister, host, authToken, creator string) error {
	return runWithTUI(parentCtx, withoutHeader(), func(ctx context.Context, sink output.Sink) error {
		return snapshot.List(ctx, rt, containers, lister, host, authToken, creator, sink)
	})
}
