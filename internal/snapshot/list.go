package snapshot

import (
	"context"
	"fmt"

	"github.com/localstack/lstk/internal/api"
	"github.com/localstack/lstk/internal/output"
)

type CloudPodLister interface {
	ListCloudPods(ctx context.Context, authToken, creator string) ([]api.CloudPod, error)
}

func List(ctx context.Context, lister CloudPodLister, authToken, creator string, sink output.Sink) error {
	if authToken == "" {
		sink.Emit(output.ErrorEvent{
			Title: "Authentication required to list snapshots",
			Actions: []output.ErrorAction{
				{Label: "Log in:", Value: "lstk login"},
				{Label: "Or set a token:", Value: "export LOCALSTACK_AUTH_TOKEN=<token>"},
			},
		})
		return output.NewSilentError(fmt.Errorf("authentication required: no auth token"))
	}

	sink.Emit(output.SpinnerStart("Fetching snapshots"))
	pods, err := lister.ListCloudPods(ctx, authToken, creator)
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
