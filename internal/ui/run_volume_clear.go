package ui

import (
	"context"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/volume"
)

func RunVolumeClear(parentCtx context.Context, containers []config.ContainerConfig) error {
	return runWithTUI(parentCtx, withoutHeader(), func(ctx context.Context, sink output.Sink) error {
		return volume.Clear(ctx, sink, containers, false)
	})
}
