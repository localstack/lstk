package container

import (
	"context"

	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
)

func Restart(ctx context.Context, rt runtime.Runtime, sink output.Sink, stopOpts StopOptions, startOpts StartOptions, interactive bool) error {
	if err := Stop(ctx, rt, sink, startOpts.Containers, stopOpts); err != nil {
		return err
	}
	_, err := Start(ctx, rt, sink, startOpts, interactive)
	return err
}
