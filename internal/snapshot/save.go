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
	ExportState(ctx context.Context, host string, dst io.Writer) error
}

// PodSaveResult holds the metadata returned by the platform after a successful pod save.
type PodSaveResult struct {
	Version  int
	Services []string
	Size     int64
}

// PodSaver triggers a remote pod snapshot save on the running LocalStack instance.
type PodSaver interface {
	SavePodSnapshot(ctx context.Context, host, podName, authToken string) (PodSaveResult, error)
}

func save(ctx context.Context, rt runtime.Runtime, containers []config.ContainerConfig, sink output.Sink, host, spinnerText string, onSuccess func(), do func() error) (retErr error) {
	_, resolved, err := container.FirstReachableEmulator(ctx, rt, sink, containers, host)
	if err != nil {
		return err
	}
	if !resolved.Found() {
		sink.Emit(output.ErrorEvent{
			Title: "LocalStack is not running",
			Actions: []output.ErrorAction{
				{Label: "Start LocalStack:", Value: "lstk"},
				{Label: "See help:", Value: "lstk -h"},
			},
		})
		return output.NewSilentError(fmt.Errorf("LocalStack is not running"))
	}

	emitExperimentalWarning(containers, sink)

	sink.Emit(output.SpinnerStart(spinnerText))
	defer func() {
		sink.Emit(output.SpinnerStop())
		if retErr == nil {
			onSuccess()
		}
	}()

	return do()
}

func SaveLocal(ctx context.Context, rt runtime.Runtime, containers []config.ContainerConfig, exporter StateExporter, host, dest string, sink output.Sink) error {
	cwd, _ := os.Getwd()
	home, _ := os.UserHomeDir()
	return save(ctx, rt, containers, sink, host,
		"Saving snapshot...",
		func() {
			sink.Emit(output.MessageEvent{Severity: output.SeveritySuccess, Text: fmt.Sprintf("Snapshot saved to %s", displayPath(dest, cwd, home))})
		},
		func() error {
			w, err := os.Create(dest)
			if err != nil {
				return fmt.Errorf("save to %s: %w", dest, err)
			}
			if err := exporter.ExportState(ctx, host, w); err != nil {
				_ = w.Close()
				_ = os.Remove(dest)
				return fmt.Errorf("export state from LocalStack: %w", err)
			}
			return w.Close()
		},
	)
}

func SavePod(ctx context.Context, rt runtime.Runtime, containers []config.ContainerConfig, saver PodSaver, host, podName, authToken string, sink output.Sink) error {
	if authToken == "" {
		return fmt.Errorf("pod snapshots require authentication — set LOCALSTACK_AUTH_TOKEN or run %q", "lstk login")
	}
	var result PodSaveResult
	return save(ctx, rt, containers, sink, host,
		fmt.Sprintf("Saving snapshot to pod %q...", podName),
		func() {
			sink.Emit(output.PodSnapshotSavedEvent{
				PodName:  podName,
				Version:  result.Version,
				Services: result.Services,
				Size:     result.Size,
			})
		},
		func() error {
			var err error
			result, err = saver.SavePodSnapshot(ctx, host, podName, authToken)
			return err
		},
	)
}
