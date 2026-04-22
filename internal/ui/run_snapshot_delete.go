package ui

import (
	"context"

	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/snapshot"
)

func RunSnapshotDelete(parentCtx context.Context, apiEndpoint, token, name string) error {
	return runWithTUI(parentCtx, withoutHeader(), func(ctx context.Context, sink output.Sink) error {
		client := snapshot.NewPlatformClient(apiEndpoint, token)
		return snapshot.Delete(ctx, client, sink, name, false)
	})
}
