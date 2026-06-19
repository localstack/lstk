package ui

import (
	"context"

	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/snapshot"
)

func RunSnapshotInspect(parentCtx context.Context, path string) error {
	return runWithTUI(parentCtx, withoutHeader(), func(ctx context.Context, sink output.Sink) error {
		return snapshot.Inspect(path, sink)
	})
}
