package snapshot

import (
	"context"
	"fmt"
	"os"

	"github.com/localstack/lstk/internal/output"
)

// Import loads a local ZIP snapshot file into the running emulator.
func Import(ctx context.Context, client *EmulatorClient, sink output.Sink, opts ImportOptions) (*ImportResult, error) {
	info, err := os.Stat(opts.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("snapshot file not found: %s", opts.Path)
		}
		return nil, fmt.Errorf("failed to read snapshot file: %w", err)
	}
	size := info.Size()

	f, err := os.Open(opts.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to open snapshot file: %w", err)
	}
	defer func() { _ = f.Close() }()

	onProgress := func(done, total int64) {
		output.EmitSnapshotTransfer(sink, "upload", done, total)
	}

	var services []string
	onEvent := func(ev streamEvent) {
		switch ev.Event {
		case "service":
			output.EmitSnapshotService(sink, ev.Service, ev.Status, "import")
			if ev.Status == "ok" {
				services = append(services, ev.Service)
			}
		case "completion":
			if ev.Info != nil && len(ev.Info.Services) > 0 {
				services = ev.Info.Services
			}
		}
	}

	if err := client.Import(ctx, f, size, onProgress, onEvent); err != nil {
		return nil, err
	}

	return &ImportResult{
		Path:     opts.Path,
		Services: services,
	}, nil
}
