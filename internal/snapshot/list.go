package snapshot

import (
	"context"
	"fmt"
	"time"

	"github.com/localstack/lstk/internal/output"
)

// List retrieves and emits the authenticated user's remote snapshots as a table.
func List(ctx context.Context, client *PlatformClient, sink output.Sink) ([]PodInfo, error) {
	pods, err := client.ListPods(ctx)
	if err != nil {
		return nil, err
	}

	if len(pods) == 0 {
		output.EmitNote(sink, "No snapshots found. Use 'lstk snapshot save <name>' to create one.")
		return pods, nil
	}

	rows := make([][]string, len(pods))
	for i, p := range pods {
		rows[i] = []string{
			p.Name,
			fmt.Sprintf("%d", p.MaxVersion),
			formatRelativeTime(p.LastChange),
			formatBytes(p.StorageSize),
		}
	}

	output.EmitTable(sink, output.TableEvent{
		Headers: []string{"Name", "Versions", "Last Modified", "Size"},
		Rows:    rows,
	})
	return pods, nil
}

func formatRelativeTime(unix int64) string {
	if unix == 0 {
		return "—"
	}
	d := time.Since(time.Unix(unix, 0))
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", h)
	case d < 48*time.Hour:
		return "yesterday"
	case d < 7*24*time.Hour:
		days := int(d.Hours() / 24)
		return fmt.Sprintf("%d days ago", days)
	case d < 14*24*time.Hour:
		return "last week"
	default:
		weeks := int(d.Hours() / (24 * 7))
		return fmt.Sprintf("%d weeks ago", weeks)
	}
}
