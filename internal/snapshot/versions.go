package snapshot

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/localstack/lstk/internal/api"
	"github.com/localstack/lstk/internal/output"
)

// CloudPodVersionLister is satisfied by api.PlatformClient.
type CloudPodVersionLister interface {
	GetCloudPodVersions(ctx context.Context, authToken, podName string) ([]api.CloudPodVersion, error)
}

// Versions lists a cloud snapshot's version history as a table. Like Show, it is
// cloud-only, requires authentication, and reads the LocalStack platform API
// directly — it never contacts the emulator, so no emulator need be running.
func Versions(ctx context.Context, lister CloudPodVersionLister, authToken, podName string, sink output.Sink) error {
	if authToken == "" {
		sink.Emit(output.ErrorEvent{
			Title: "Authentication required to list snapshot versions",
			Actions: []output.ErrorAction{
				{Label: "Log in:", Value: "lstk login"},
				{Label: "Or set a token:", Value: "export LOCALSTACK_AUTH_TOKEN=<token>"},
			},
		})
		return output.NewSilentError(fmt.Errorf("authentication required: no auth token"))
	}

	sink.Emit(output.SpinnerStart("Fetching versions"))
	versions, err := lister.GetCloudPodVersions(ctx, authToken, podName)
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
		return fmt.Errorf("list snapshot versions: %w", err)
	}

	if len(versions) == 0 {
		sink.Emit(output.DeferredEvent{Inner: output.MessageEvent{
			Severity: output.SeverityNote,
			Text:     fmt.Sprintf("No versions found for 'pod:%s'", podName),
		}})
		return nil
	}

	noun := "versions"
	if len(versions) == 1 {
		noun = "version"
	}
	rows := make([][]string, len(versions))
	for i, v := range versions {
		created := "-"
		if v.Created != nil {
			created = v.Created.UTC().Format("2006-01-02 15:04 UTC")
		}
		rows[i] = []string{
			strconv.Itoa(v.Version),
			created,
			orDash(v.LocalStackVersion),
			orDash(strings.Join(v.Services, ", ")),
		}
	}
	sink.Emit(output.DeferredEvent{Inner: output.MessageEvent{Severity: output.SeveritySecondary, Text: fmt.Sprintf("~ %d %s\n", len(versions), noun)}})
	// Services is passed in full and left as the widest column on purpose: it is
	// the one formatTableWidth shrinks to fit the terminal, so a wide terminal
	// shows every service name and a redirected stdout (width 0) skips truncation
	// entirely. The description field is deliberately not a column — nothing in
	// lstk ever sets it, so it rendered as "-" on every row.
	sink.Emit(output.DeferredEvent{Inner: output.TableEvent{
		Headers: []string{"Version", "Created", "LocalStack", "Services"},
		Rows:    rows,
	}})
	return nil
}

// orDash renders an empty cell as "-" so a row never has visually missing columns.
func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
