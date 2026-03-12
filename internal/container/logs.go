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

func Logs(ctx context.Context, rt runtime.Runtime, sink output.Sink, containers []config.ContainerConfig, follow bool) error {
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
		output.EmitLogLine(sink, output.LogSourceEmulator, scanner.Text())
	}
	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		return err
	}
	return <-errCh
}
