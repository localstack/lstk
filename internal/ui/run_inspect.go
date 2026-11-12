package ui

import (
	"context"

	"github.com/localstack/lstk/internal/inspect"
	"github.com/localstack/lstk/internal/output"
)

func RunInspectSet(parentCtx context.Context, t inspect.Target, enabled bool) error {
	return runWithTUI(parentCtx, withoutHeader(), func(ctx context.Context, sink output.Sink) error {
		return inspect.Set(ctx, sink, t, enabled)
	})
}
