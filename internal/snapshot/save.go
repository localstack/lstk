package snapshot

//go:generate mockgen -source=save.go -destination=mock_state_exporter_test.go -package=snapshot_test

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/container"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
)

// StateExporter retrieves state from the running LocalStack instance.
type StateExporter interface {
	ExportState(ctx context.Context, host string) (io.ReadCloser, error)
}

func Save(ctx context.Context, rt runtime.Runtime, containers []config.ContainerConfig, exporter StateExporter, host, dest string, sink output.Sink) (retErr error) {
	if err := rt.IsHealthy(ctx); err != nil {
		rt.EmitUnhealthyError(sink, err)
		return output.NewSilentError(fmt.Errorf("runtime not healthy: %w", err))
	}

	runningContainers, err := container.RunningEmulators(ctx, rt, containers)
	if err != nil {
		return fmt.Errorf("checking emulator status: %w", err)
	}
	if len(runningContainers) == 0 {
		sink.Emit(output.ErrorEvent{
			Title: "LocalStack is not running",
			Actions: []output.ErrorAction{
				{Label: "Start LocalStack:", Value: "lstk"},
				{Label: "See help:", Value: "lstk -h"},
			},
		})
		return output.NewSilentError(fmt.Errorf("LocalStack is not running"))
	}

	sink.Emit(output.SpinnerStart("Saving snapshot..."))
	defer func() {
		sink.Emit(output.SpinnerStop())
		if retErr == nil {
			sink.Emit(output.MessageEvent{Severity: output.SeveritySuccess, Text: fmt.Sprintf("Snapshot saved to %s", dest)})
		}
	}()

	body, err := exporter.ExportState(ctx, host)
	if err != nil {
		return fmt.Errorf("export state from LocalStack: %w", err)
	}
	defer func() { _ = body.Close() }()

	w, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("save to %s: %w", dest, err)
	}

	if _, err := io.Copy(w, body); err != nil {
		_ = w.Close()
		_ = os.Remove(dest)
		return fmt.Errorf("write snapshot: %w", err)
	}

	return w.Close()
}
