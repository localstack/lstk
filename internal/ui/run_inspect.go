package ui

import (
	"context"

	"github.com/localstack/lstk/internal/inspect"
	"github.com/localstack/lstk/internal/output"
)

func RunInspectSet(parentCtx context.Context, t inspect.Target, enabled, persist bool) error {
	return runWithTUI(parentCtx, withoutHeader(), func(ctx context.Context, sink output.Sink) error {
		return inspect.Set(ctx, sink, t, enabled, persist)
	})
}

func RunInspectStatus(parentCtx context.Context, t inspect.Target) error {
	return runWithTUI(parentCtx, withoutHeader(), func(ctx context.Context, sink output.Sink) error {
		return inspect.Status(ctx, sink, t)
	})
}
