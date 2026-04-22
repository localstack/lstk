package snapshot

import (
	"context"

	"github.com/localstack/lstk/internal/output"
)

// Delete removes a remote snapshot and all its versions from the platform.
func Delete(ctx context.Context, client *PlatformClient, sink output.Sink, name string) error {
	if err := client.DeletePod(ctx, name); err != nil {
		return err
	}
	output.EmitSuccess(sink, "Snapshot '"+name+"' deleted")
	return nil
}
