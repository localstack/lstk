package container

import (
	"context"
	"fmt"

	"github.com/docker/docker/errdefs"
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
			return &StopError{Name: name, Err: err}
		}
		onProgress(fmt.Sprintf("%s stopped", name))
	}

	return nil
}

type StopError struct {
	Name string
	Err  error
}

func (e *StopError) Error() string {
	if errdefs.IsNotFound(e.Err) {
		return fmt.Sprintf("%s is not running", e.Name)
	}
	return fmt.Sprintf("Failed to stop %s\n%s", e.Name, e.Err.Error())
}
