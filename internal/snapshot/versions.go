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

// Column budgets for the versions table, in characters. formatTableWidth shrinks
// only its single widest column and never below 10 chars, so a table with two
// free-text columns (services and description) has to bound them here or it
// overflows a narrow terminal. The fixed columns cost 67 chars at worst
// (2 indent + 4x2 gaps + 7 "VERSION" + 20 timestamp + 10 "LOCALSTACK" + this
// services budget), which leaves the description column above that 10-char floor
// on an 80-column terminal.
const (
	maxServicesLen    = 20
	maxDescriptionLen = 40
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
			orDash(truncateServices(v.Services, maxServicesLen)),
			orDash(truncateDescription(v.Description, maxDescriptionLen)),
		}
	}
	sink.Emit(output.DeferredEvent{Inner: output.MessageEvent{Severity: output.SeveritySecondary, Text: fmt.Sprintf("~ %d %s\n", len(versions), noun)}})
	sink.Emit(output.DeferredEvent{Inner: output.TableEvent{
		Headers: []string{"Version", "Created", "LocalStack", "Services", "Description"},
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

// truncateServices joins as many service names as fit within max characters,
// summarising the remainder as "+N more". The budget counts characters rather
// than service names because service names vary in length and it is the rendered
// width that has to stay bounded. At least one name is always shown (hard-cut if
// need be) so the cell identifies something rather than collapsing to "+N more".
//
// The result never exceeds max characters.
func truncateServices(services []string, max int) string {
	if len(services) == 0 {
		return ""
	}
	if joined := strings.Join(services, ", "); len([]rune(joined)) <= max {
		return joined
	}
	// The "+N more" suffix width depends on how many names are hidden, which
	// depends on how many fit — so walk counts downwards until one fits.
	for shown := len(services) - 1; shown >= 1; shown-- {
		body := strings.Join(services[:shown], ", ")
		suffix := fmt.Sprintf(" +%d more", len(services)-shown)
		if len([]rune(body))+len([]rune(suffix)) <= max {
			return body + suffix
		}
	}
	// Not even one full name fits alongside the suffix.
	suffix := fmt.Sprintf(" +%d more", len(services)-1)
	if budget := max - len([]rune(suffix)); budget > 0 {
		return truncate(services[0], budget) + suffix
	}
	return truncate(services[0], max)
}

// truncateDescription bounds a free-text description to max characters.
func truncateDescription(description string, max int) string {
	return truncate(description, max)
}

// truncate shortens s to at most max characters, counting runes so multi-byte
// characters are never split, and counting the ellipsis that marks the cut — so
// the result is never wider than max.
func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max <= 0 {
		return ""
	}
	if max == 1 {
		return "…"
	}
	return strings.TrimRight(string(runes[:max-1]), " ") + "…"
}
