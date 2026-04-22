package ui

import (
	"context"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/container"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
)

func RunStop(parentCtx context.Context, rt runtime.Runtime, containers []config.ContainerConfig, opts container.StopOptions) error {
	return runWithTUI(parentCtx, withoutHeader(), func(ctx context.Context, sink output.Sink) error {
		return container.Stop(ctx, rt, sink, containers, opts)
	})
}
