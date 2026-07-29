package cmd

import (
	"fmt"

	"github.com/localstack/lstk/internal/endpoint"
	"github.com/localstack/lstk/internal/output"
	"github.com/spf13/cobra"
)

// rejectExplicitEndpointURL returns an actionable error if --endpoint-url was
// explicitly passed on the command line for a command that has no remote
// equivalent (the Docker-lifecycle/filesystem operations: logs, stop,
// restart, volume). Silently ignoring the flag here would act on the *local*
// target while the user's flag said "I mean the remote one" — a wrong-target
// risk (streaming or clearing the wrong thing), not a harmless no-op — so it
// errors instead of proceeding. An ambient LSTK_ENDPOINT_URL/AWS_ENDPOINT_URL
// (set for other commands in the same session) is not rejected here, since it
// wasn't targeted at this specific invocation — only an explicit flag is.
func rejectExplicitEndpointURL(cmd *cobra.Command, sink output.Sink, label string) error {
	f := cmd.Flags().Lookup(endpoint.FlagName)
	if f == nil || !f.Changed {
		return nil
	}
	err := fmt.Errorf("%s does not support --endpoint-url: it operates on a local Docker container or local filesystem state with no remote equivalent", label)
	sink.Emit(output.ErrorEvent{Title: err.Error()})
	return output.NewSilentError(err)
}
