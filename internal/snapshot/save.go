package snapshot

import (
	"context"
	"fmt"
	"strings"

	"github.com/localstack/lstk/internal/output"
)

// Save saves the running emulator's state as a named remote snapshot on the LocalStack platform.
func Save(ctx context.Context, client *EmulatorClient, sink output.Sink, opts SaveOptions) (*SaveResult, error) {
	var result SaveResult
	result.PodName = opts.PodName

	onEvent := func(ev streamEvent) {
		switch ev.Event {
		case "service":
			output.EmitSnapshotService(sink, ev.Service, ev.Status, "save")
			if ev.Status == "ok" {
				result.Services = append(result.Services, ev.Service)
			}
		case "completion":
			if ev.Info != nil {
				if ev.Info.Version > 0 {
					result.Version = ev.Info.Version
				}
				if len(ev.Info.Services) > 0 {
					result.Services = ev.Info.Services
				}
			}
			if ev.Status == "error" {
				// error message comes through the completion event
			}
		}
	}

	if err := client.Save(ctx, opts, onEvent); err != nil {
		return nil, err
	}

	services := strings.Join(result.Services, ", ")
	if services == "" {
		services = "all services"
	}

	versionStr := ""
	if result.Version > 0 {
		versionStr = fmt.Sprintf(" (version %d)", result.Version)
	}

	output.EmitSuccess(sink, fmt.Sprintf("Snapshot '%s' saved to remote%s", opts.PodName, versionStr))
	output.EmitSecondary(sink, "Services: "+services)
	return &result, nil
}
