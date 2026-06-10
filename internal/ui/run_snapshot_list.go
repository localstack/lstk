package ui

import (
	"context"

	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/snapshot"
)

func RunSnapshotList(parentCtx context.Context, lister snapshot.CloudPodLister, authToken, creator string) error {
	return runWithTUI(parentCtx, withoutHeader(), func(ctx context.Context, sink output.Sink) error {
		return snapshot.List(ctx, lister, authToken, creator, sink)
	})
}
