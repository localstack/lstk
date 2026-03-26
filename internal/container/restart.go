package container

import (
	"context"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
)

func Restart(ctx context.Context, rt runtime.Runtime, sink output.Sink, containers []config.ContainerConfig, startOpts StartOptions, stopOpts StopOptions) error {
	anyRunning, err := AnyRunning(ctx, rt, containers)
	if err != nil {
		return err
	}
	if !anyRunning {
		output.EmitInfo(sink, "LocalStack is not running")
		return nil
	}

	if err := Stop(ctx, rt, sink, containers, stopOpts); err != nil {
		return err
	}

	startOpts.SkipPull = true
	return Start(ctx, rt, sink, startOpts, false)
}
