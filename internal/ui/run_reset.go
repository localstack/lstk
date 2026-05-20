package ui

import (
	"context"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/reset"
	"github.com/localstack/lstk/internal/runtime"
)

func RunReset(parentCtx context.Context, rt runtime.Runtime, containers []config.ContainerConfig, resetter reset.StateResetter, host string, force bool) error {
	return runWithTUI(parentCtx, withoutHeader(), func(ctx context.Context, sink output.Sink) error {
		return reset.Reset(ctx, rt, containers, resetter, host, force, sink)
	})
}
