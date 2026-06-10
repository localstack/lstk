package snapshot

import (
	"context"
	"fmt"
	"time"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/container"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
)

// PodSnapshot holds metadata for a single Cloud Pod returned by the list endpoint.
type PodSnapshot struct {
	Name        string
	Version     int
	LastChanged *time.Time
}

// PodLister retrieves available Cloud Pods from the running LocalStack instance.
type PodLister interface {
	ListPodSnapshots(ctx context.Context, host, authToken, creator string) ([]PodSnapshot, error)
}

// List fetches Cloud Pod snapshots from the running emulator and emits a SnapshotListEvent.
// creator filters results server-side: "me" for the current user's pods, "" for all org pods.
func List(ctx context.Context, rt runtime.Runtime, containers []config.ContainerConfig, lister PodLister, host, authToken, creator string, sink output.Sink) error {
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

	sink.Emit(output.SpinnerStart("Fetching snapshots"))
	pods, err := lister.ListPodSnapshots(ctx, host, authToken, creator)
	sink.Emit(output.SpinnerStop())
	if err != nil {
		return fmt.Errorf("list snapshots: %w", err)
	}

	entries := make([]output.PodSnapshotEntry, len(pods))
	for i, p := range pods {
		entries[i] = output.PodSnapshotEntry{
			Name:        p.Name,
			Version:     p.Version,
			LastChanged: p.LastChanged,
		}
	}
	sink.Emit(output.SnapshotListEvent{Pods: entries})
	return nil
}
