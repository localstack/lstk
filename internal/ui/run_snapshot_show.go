package ui

import (
	"context"

	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/snapshot"
)

func RunSnapshotShow(parentCtx context.Context, inspector snapshot.CloudPodInspector, authToken, podName string) error {
	return runWithTUI(parentCtx, withoutHeader(), func(ctx context.Context, sink output.Sink) error {
		return snapshot.Show(ctx, inspector, authToken, podName, sink)
	})
}
