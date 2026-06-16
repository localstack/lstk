package snapshot

import (
	"context"
	"errors"
	"fmt"

	"github.com/localstack/lstk/internal/api"
	"github.com/localstack/lstk/internal/output"
)

type CloudPodInspector interface {
	GetCloudPod(ctx context.Context, authToken, podName string) (*api.CloudPodDetails, error)
}

// Show fetches a single cloud snapshot's metadata from the platform and emits it
// as a SnapshotShownEvent. It is cloud-only and requires authentication.
func Show(ctx context.Context, inspector CloudPodInspector, authToken, podName string, sink output.Sink) error {
	if authToken == "" {
		sink.Emit(output.ErrorEvent{
			Title: "Authentication required to show snapshots",
			Actions: []output.ErrorAction{
				{Label: "Log in:", Value: "lstk login"},
				{Label: "Or set a token:", Value: "export LOCALSTACK_AUTH_TOKEN=<token>"},
			},
		})
		return output.NewSilentError(fmt.Errorf("authentication required: no auth token"))
	}

	sink.Emit(output.SpinnerStart("Fetching snapshot"))
	details, err := inspector.GetCloudPod(ctx, authToken, podName)
	sink.Emit(output.SpinnerStop())
	if err != nil {
		if errors.Is(err, api.ErrCloudPodNotFound) {
			sink.Emit(output.ErrorEvent{
				Title: fmt.Sprintf("Snapshot 'pod:%s' not found", podName),
				Actions: []output.ErrorAction{
					{Label: "List your snapshots:", Value: "lstk snapshot list"},
				},
			})
			return output.NewSilentError(err)
		}
		return fmt.Errorf("show snapshot: %w", err)
	}

	sink.Emit(output.DeferredEvent{Inner: toShownEvent(details)})
	return nil
}

func toShownEvent(d *api.CloudPodDetails) output.SnapshotShownEvent {
	resources := make([]output.SnapshotResourceLine, len(d.Resources))
	for i, r := range d.Resources {
		counts := make([]output.SnapshotResourceCount, len(r.Counts))
		for j, c := range r.Counts {
			counts[j] = output.SnapshotResourceCount{Count: c.Count, Noun: c.Noun}
		}
		resources[i] = output.SnapshotResourceLine{Service: r.Service, Counts: counts}
	}
	return output.SnapshotShownEvent{
		Name:              d.Name,
		Created:           d.Created,
		Size:              d.Size,
		LocalStackVersion: d.LocalStackVersion,
		Message:           d.Message,
		Services:          d.Services,
		Resources:         resources,
	}
}
