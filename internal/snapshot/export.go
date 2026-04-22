package snapshot

import (
	"context"
	"fmt"
	"os"

	"github.com/localstack/lstk/internal/output"
)

// Export saves the running emulator's state to a local ZIP file.
func Export(ctx context.Context, client *EmulatorClient, sink output.Sink, opts ExportOptions) (*ExportResult, error) {
	f, err := os.Create(opts.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to create snapshot file: %w", err)
	}
	defer func() { _ = f.Close() }()

	var bytesWritten int64
	onProgress := func(done, total int64) {
		bytesWritten = done
		output.EmitSnapshotTransfer(sink, "download", done, total)
	}

	if err := client.Export(ctx, opts.Services, f, onProgress); err != nil {
		_ = os.Remove(opts.Path)
		return nil, err
	}

	if bytesWritten == 0 {
		info, err := f.Stat()
		if err == nil {
			bytesWritten = info.Size()
		}
	}

	return &ExportResult{
		Path:  opts.Path,
		Bytes: bytesWritten,
	}, nil
}
