package snapshot

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/localstack/lstk/internal/output"
)

// Versions retrieves and emits the versions of a remote snapshot as a table.
func Versions(ctx context.Context, client *PlatformClient, sink output.Sink, name string) ([]VersionInfo, error) {
	pod, err := client.GetPod(ctx, name)
	if err != nil {
		return nil, err
	}

	if len(pod.Versions) == 0 {
		output.EmitNote(sink, fmt.Sprintf("No versions found for snapshot '%s'.", name))
		return nil, nil
	}

	rows := make([][]string, len(pod.Versions))
	for i, v := range pod.Versions {
		lsVersion := v.LocalStackVersion
		if lsVersion == "" {
			lsVersion = "—"
		}
		services := strings.Join(v.Services, ", ")
		if services == "" {
			services = "—"
		}
		desc := v.Description
		if desc == "" {
			desc = "—"
		}
		rows[i] = []string{
			fmt.Sprintf("%d", v.Version),
			formatCreatedAt(v.CreatedAt),
			lsVersion,
			services,
			desc,
		}
	}

	output.EmitTable(sink, output.TableEvent{
		Headers: []string{"Version", "Created", "LocalStack Version", "Services", "Description"},
		Rows:    rows,
	})
	return pod.Versions, nil
}

func formatCreatedAt(unix int64) string {
	if unix == 0 {
		return "—"
	}
	return time.Unix(unix, 0).UTC().Format("2006-01-02 15:04 UTC")
}
