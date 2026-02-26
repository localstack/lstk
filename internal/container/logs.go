package container

import (
	"bufio"
	"context"
	"fmt"
	"io"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
)

func Logs(ctx context.Context, rt runtime.Runtime, sink output.Sink, follow bool) error {
	cfg, err := config.Get()
	if err != nil {
		return fmt.Errorf("failed to get config: %w", err)
	}
	if len(cfg.Containers) == 0 {
		return fmt.Errorf("no containers configured")
	}

	// TODO: handle logs per container
	c := cfg.Containers[0]

	pr, pw := io.Pipe()
	errCh := make(chan error, 1)
	go func() {
		err := rt.StreamLogs(ctx, c.Name(), pw, follow)
		pw.CloseWithError(err)
		errCh <- err
	}()

	scanner := bufio.NewScanner(pr)
	for scanner.Scan() {
		output.EmitContainerLogLine(sink, scanner.Text())
	}
	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		return err
	}
	return <-errCh
}
