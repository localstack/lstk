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

func Logs(ctx context.Context, rt runtime.Runtime, sink output.Sink, containers []config.ContainerConfig, follow bool, verbose bool) error {
	if err := rt.IsHealthy(ctx); err != nil {
		rt.EmitUnhealthyError(sink, err)
		return output.NewSilentError(fmt.Errorf("runtime not healthy: %w", err))
	}

	if len(containers) == 0 {
		return fmt.Errorf("no containers configured")
	}

	// TODO: handle logs per container
	c := containers[0]

	pr, pw := io.Pipe()
	errCh := make(chan error, 1)
	go func() {
		err := rt.StreamLogs(ctx, c.Name(), pw, follow)
		pw.CloseWithError(err)
		errCh <- err
	}()

	scanner := bufio.NewScanner(pr)
	for scanner.Scan() {
		line := scanner.Text()
		if !verbose && shouldFilter(line) {
			continue
		}
		level, _ := parseLogLine(line)
		output.EmitLogLine(sink, output.LogSourceEmulator, line, level)
	}
	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		return err
	}
	return <-errCh
}
