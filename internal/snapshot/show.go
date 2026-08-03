package snapshot

import (
	"context"
	"errors"
	"fmt"

	"github.com/localstack/lstk/internal/api"
	"github.com/localstack/lstk/internal/output"
)

type CloudPodInspector interface {
	// GetCloudPod reports one version's metadata; version 0 means the latest.
	GetCloudPod(ctx context.Context, authToken, podName string, version int) (*api.CloudPodDetails, error)
}

// Show fetches a single cloud snapshot's metadata from the platform and emits it
// as a SnapshotShownEvent. It is cloud-only and requires authentication.
// version 0 shows the latest version; a non-zero version shows that exact one.
func Show(ctx context.Context, inspector CloudPodInspector, authToken, podName string, version int, sink output.Sink) error {
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

	spinnerText := "Fetching snapshot"
	if version > 0 {
		spinnerText = fmt.Sprintf("Fetching snapshot version %d", version)
	}
	sink.Emit(output.SpinnerStart(spinnerText))
	details, err := inspector.GetCloudPod(ctx, authToken, podName, version)
	sink.Emit(output.SpinnerStop())
	if err != nil {
		if errors.Is(err, api.ErrCloudPodsForbidden) {
			return emitFeatureUnavailableError(sink)
		}
		if errors.Is(err, api.ErrCloudPodNotFound) {
			sink.Emit(output.ErrorEvent{
				Title: fmt.Sprintf("Snapshot 'pod:%s' not found", podName),
				Actions: []output.ErrorAction{
					{Label: "List your snapshots:", Value: "lstk snapshot list"},
				},
			})
			return output.NewSilentError(err)
		}
		// The pod exists but that version does not — point at its version list
		// rather than the list of pods.
		var versionErr *api.CloudPodVersionNotFoundError
		if errors.As(err, &versionErr) {
			sink.Emit(output.ErrorEvent{
				Title:   fmt.Sprintf("Version %d of 'pod:%s' not found", version, podName),
				Summary: fmt.Sprintf("The highest available version is %d.", versionErr.MaxVersion),
				Actions: []output.ErrorAction{
					{Label: "List available versions:", Value: "lstk snapshot versions pod:" + podName},
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
		Version:           d.Version,
		Created:           d.Created,
		Size:              d.Size,
		LocalStackVersion: d.LocalStackVersion,
		Message:           d.Message,
		Services:          d.Services,
		Resources:         resources,
	}
}
