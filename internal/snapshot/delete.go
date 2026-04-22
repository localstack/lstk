package snapshot

import (
	"context"

	"github.com/localstack/lstk/internal/output"
)

// Delete removes a remote snapshot and all its versions from the platform.
// If skipConfirm is false, it emits a UserInputRequestEvent and waits for confirmation before proceeding.
func Delete(ctx context.Context, client *PlatformClient, sink output.Sink, name string, skipConfirm bool) error {
	if !skipConfirm {
		responseCh := make(chan output.InputResponse, 1)
		output.EmitUserInputRequest(sink, output.UserInputRequestEvent{
			Prompt: "Delete '" + name + "' and all its versions? This cannot be undone",
			Options: []output.InputOption{
				{Key: "y", Label: "Yes"},
				{Key: "n", Label: "No"},
			},
			ResponseCh: responseCh,
		})
		select {
		case resp := <-responseCh:
			if resp.Cancelled || resp.SelectedKey != "y" {
				output.EmitNote(sink, "Deletion cancelled.")
				return nil
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	if err := client.DeletePod(ctx, name); err != nil {
		return err
	}
	output.EmitSuccess(sink, "Snapshot '"+name+"' deleted")
	return nil
}
