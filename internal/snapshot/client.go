package snapshot

import (
	"context"
	"io"
)

// StateExporter retrieves state from the running LocalStack instance.
type StateExporter interface {
	ExportState(ctx context.Context, host string) (io.ReadCloser, error)
}
