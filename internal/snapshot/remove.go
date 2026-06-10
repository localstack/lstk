package snapshot

import (
	"context"
	"errors"
	"fmt"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/container"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
)

// ErrPodNotFound is returned when the cloud pod does not exist on the platform.
var ErrPodNotFound = errors.New("cloud pod not found")

// PodRemover deletes a remote pod snapshot on the LocalStack platform.
type PodRemover interface {
	RemovePodSnapshot(ctx context.Context, host, podName, authToken string) error
}

// Remove deletes a remote pod snapshot, prompting for confirmation unless force is true.
func Remove(ctx context.Context, rt runtime.Runtime, containers []config.ContainerConfig, podName, authToken string, remover PodRemover, host string, force bool, sink output.Sink) error {
	if authToken == "" {
		return fmt.Errorf("pod snapshots require authentication — set LOCALSTACK_AUTH_TOKEN or run %q", "lstk login")
	}

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

	if !force {
		responseCh := make(chan output.InputResponse, 1)
		sink.Emit(output.UserInputRequestEvent{
			Prompt: fmt.Sprintf("Delete cloud snapshot 'pod:%s'? This operation cannot be undone.", podName),
			Options: []output.InputOption{
				{Key: "y", Label: "Y"},
				{Key: "n", Label: "n"},
			},
			ResponseCh: responseCh,
		})

		select {
		case resp := <-responseCh:
			if resp.Cancelled || resp.SelectedKey != "y" {
				sink.Emit(output.MessageEvent{Severity: output.SeverityNote, Text: "Cancelled"})
				return nil
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return remove(ctx, podName, authToken, remover, host, sink)
}

func remove(ctx context.Context, podName, authToken string, remover PodRemover, host string, sink output.Sink) (retErr error) {
	sink.Emit(output.SpinnerStart(fmt.Sprintf("Deleting snapshot 'pod:%s'...", podName)))
	defer func() {
		sink.Emit(output.SpinnerStop())
		if retErr == nil {
			sink.Emit(output.PodSnapshotRemovedEvent{PodName: podName})
		}
	}()
	err := remover.RemovePodSnapshot(ctx, host, podName, authToken)
	if errors.Is(err, ErrPodNotFound) {
		return fmt.Errorf("cloud pod %q not found", podName)
	}
	return err
}
