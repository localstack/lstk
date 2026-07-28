//go:generate mockgen -source=diff.go -destination=mock_diff_client_test.go -package=snapshot_test

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

// ServiceDiffCounts holds the addition and modification counts for a single service.
type ServiceDiffCounts struct {
	Additions     int
	Modifications int
}

// DiffResult maps service name to addition/modification counts from a diff response.
type DiffResult map[string]ServiceDiffCounts

// PodDiffer is satisfied by aws.Client.
type PodDiffer interface {
	// DiffPodSnapshot issues GET /_localstack/pods/{name}/diff. version 0 diffs
	// against the pod's latest version.
	DiffPodSnapshot(ctx context.Context, host, podName string, version int, authToken string) (DiffResult, error)
}

// DiffPod calls the diff endpoint for a named pod and emits a SnapshotDiffEvent.
// It requires the emulator to already be running (unlike LoadPod, there is no auto-start).
// version 0 diffs against the pod's latest version.
// An empty authToken is intentional for a locally managed emulator; see ErrAuthRequired.
func DiffPod(ctx context.Context, rt runtime.Runtime, containers []config.ContainerConfig, differ PodDiffer, host, podName string, version int, authToken, strategy string, sink output.Sink) error {
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

	spinnerText := fmt.Sprintf("Checking diff for pod %q...", podName)
	if version > 0 {
		spinnerText = fmt.Sprintf("Checking diff for pod %q (version %d)...", podName, version)
	}
	sink.Emit(output.SpinnerStart(spinnerText))
	result, err := differ.DiffPodSnapshot(ctx, host, podName, version, authToken)
	sink.Emit(output.SpinnerStop())
	if errors.Is(err, ErrSnapshotFeatureUnavailable) {
		return emitFeatureUnavailableError(sink)
	}
	if errors.Is(err, ErrPodVersionNotFound) {
		return emitPodVersionNotFound(err, podName, "Could not check pod diff", sink)
	}
	if errors.Is(err, ErrAuthRequired) {
		return emitAuthRequired(sink, err)
	}
	if errors.Is(err, ErrPodNotFound) {
		sink.Emit(output.ErrorEvent{
			Title:   "Could not check pod diff",
			Summary: "Snapshot was not found on the LocalStack platform",
			Actions: []output.ErrorAction{
				{Label: "List your snapshots:", Value: "lstk snapshot list"},
			},
		})
		return output.NewSilentError(err)
	}
	if err != nil {
		return err
	}

	services := make(map[string]output.SnapshotDiffServiceResult, len(result))
	for svc, counts := range result {
		services[svc] = output.SnapshotDiffServiceResult{
			Additions:     counts.Additions,
			Modifications: counts.Modifications,
		}
	}
	sink.Emit(output.SnapshotDiffEvent{
		PodName:  podName,
		Version:  version,
		Strategy: strategy,
		Services: services,
	})
	return nil
}
