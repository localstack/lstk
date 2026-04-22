package ui

import (
	"context"

	"github.com/localstack/lstk/internal/container"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
)

func RunRestart(parentCtx context.Context, rt runtime.Runtime, stopOpts container.StopOptions, startOpts container.StartOptions) error {
	return runWithTUI(parentCtx, withoutHeader(), func(ctx context.Context, sink output.Sink) error {
		return container.Restart(ctx, rt, sink, stopOpts, startOpts, true)
	})
}
