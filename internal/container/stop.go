package container

import (
	"context"
	"fmt"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/runtime"
)

func Stop(ctx context.Context, rt runtime.Runtime, onProgress func(string)) error {
	cfg, err := config.Get()
	if err != nil {
		return fmt.Errorf("failed to get config: %w", err)
	}

	for _, c := range cfg.Containers {
		name := c.Name()
		onProgress(fmt.Sprintf("Stopping %s...", name))
		if err := rt.Stop(ctx, name); err != nil {
			return fmt.Errorf("failed to stop %s: %w", name, err)
		}
		onProgress(fmt.Sprintf("%s stopped", name))
	}

	return nil
}
