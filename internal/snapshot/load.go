package snapshot

import (
	"context"
	"fmt"
	"strings"

	"github.com/localstack/lstk/internal/output"
)

// Load loads a remote snapshot into the running emulator.
func Load(ctx context.Context, client *EmulatorClient, sink output.Sink, opts LoadOptions) (*LoadResult, error) {
	var result LoadResult
	result.PodName = opts.PodName

	onEvent := func(ev streamEvent) {
		switch ev.Event {
		case "service":
			output.EmitSnapshotService(sink, ev.Service, ev.Status, "load")
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
		}
	}

	if err := client.Load(ctx, opts, onEvent); err != nil {
		return nil, err
	}

	services := strings.Join(result.Services, ", ")
	if services == "" {
		services = "all services"
	}

	versionStr := ""
	if result.Version > 0 {
		versionStr = fmt.Sprintf(" (version %d)", result.Version)
	} else if opts.Version > 0 {
		versionStr = fmt.Sprintf(" (version %d)", opts.Version)
	}

	output.EmitSuccess(sink, fmt.Sprintf("Snapshot '%s'%s loaded from remote", opts.PodName, versionStr))
	output.EmitSecondary(sink, "Services restored: "+services)
	return &result, nil
}
