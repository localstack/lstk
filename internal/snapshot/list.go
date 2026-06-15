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

	if len(pods) == 0 {
		sink.Emit(output.DeferredEvent{Inner: output.MessageEvent{Severity: output.SeverityNote, Text: "No snapshots found"}})
		return nil
	}
	noun := "snapshots"
	if len(pods) == 1 {
		noun = "snapshot"
	}
	rows := make([][]string, len(pods))
	for i, p := range pods {
		lastChanged := "-"
		if p.LastChanged != nil {
			lastChanged = p.LastChanged.UTC().Format("2006-01-02 15:04 UTC")
		}
		rows[i] = []string{p.Name, fmt.Sprintf("%d", p.Version), lastChanged}
	}
	sink.Emit(output.DeferredEvent{Inner: output.MessageEvent{Severity: output.SeveritySecondary, Text: fmt.Sprintf("~ %d %s\n", len(pods), noun)}})
	sink.Emit(output.DeferredEvent{Inner: output.TableEvent{
		Headers: []string{"Name", "Version", "Last Changed"},
		Rows:    rows,
	}})
	return nil
}
