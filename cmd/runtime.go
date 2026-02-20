package cmd

import (
	"context"

	"github.com/localstack/lstk/internal/runtime"
)

func newRuntime(ctx context.Context) (runtime.Runtime, error) {
	rt, err := runtime.NewDockerRuntime()
	if err != nil {
		return nil, err
	}
	if err := runtime.CheckContainerEngine(ctx, rt); err != nil {
		return nil, err
	}
	return rt, nil
}
