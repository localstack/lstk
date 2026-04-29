package snapshot

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

// Save exports the emulator's state via exporter and writes it to dest.
func Save(ctx context.Context, rt runtime.Runtime, containers []config.ContainerConfig, exporter StateExporter, dest string, sink output.Sink) error {
	if err := rt.IsHealthy(ctx); err != nil {
		rt.EmitUnhealthyError(sink, err)
		return output.NewSilentError(fmt.Errorf("runtime not healthy: %w", err))
	}

	running, err := container.AnyRunning(ctx, rt, containers)
	if err != nil {
		return fmt.Errorf("checking emulator status: %w", err)
	}
	if !running {
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

	body, err := exporter.ExportState(ctx)
	if err != nil {
		sink.Emit(output.SpinnerStop())
		return fmt.Errorf("export state from LocalStack: %w", err)
	}
	defer func() { _ = body.Close() }()

	w, err := os.Create(dest)
	if err != nil {
		sink.Emit(output.SpinnerStop())
		return fmt.Errorf("save to %s: %w", dest, err)
	}

	if _, err := io.Copy(w, body); err != nil {
		_ = w.Close()
		sink.Emit(output.SpinnerStop())
		return fmt.Errorf("write snapshot: %w", err)
	}

	if err := w.Close(); err != nil {
		sink.Emit(output.SpinnerStop())
		return fmt.Errorf("close snapshot: %w", err)
	}

	sink.Emit(output.SpinnerStop())
	sink.Emit(output.MessageEvent{Severity: output.SeveritySuccess, Text: fmt.Sprintf("Snapshot saved to %s", dest)})
	return nil
}
