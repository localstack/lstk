package ui

import (
	"context"

	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/snapshot"
)

func RunSnapshotVersions(parentCtx context.Context, lister snapshot.CloudPodVersionLister, authToken, podName string) error {
	return runWithTUI(parentCtx, withoutHeader(), func(ctx context.Context, sink output.Sink) error {
		return snapshot.Versions(ctx, lister, authToken, podName, sink)
	})
}
