package snapshot

//go:generate mockgen -source=client.go -destination=mock_state_exporter_test.go -package=snapshot_test

import (
	"context"
	"io"
)

// StateExporter retrieves state from the running LocalStack instance.
type StateExporter interface {
	ExportState(ctx context.Context, host string) (io.ReadCloser, error)
}
