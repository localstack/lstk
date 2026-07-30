package cmd

import (
	"fmt"
	"strings"

	"github.com/localstack/lstk/internal/endpoint"
	"github.com/localstack/lstk/internal/output"
	"github.com/spf13/cobra"
)

// rejectEndpointURL returns an actionable error if any endpoint URL source —
// an explicit --endpoint-url flag, LSTK_ENDPOINT_URL, or AWS_ENDPOINT_URL — is
// present for this invocation, for a command that has no remote equivalent
// (the Docker-lifecycle/filesystem operations: logs, stop, restart, volume,
// start).
func rejectEndpointURL(cmd *cobra.Command, sink output.Sink, label string) error {
	source, _, ok := endpoint.ResolvedSource(cmd)
	if !ok {
		return nil
	}
	how := source + " is set"
	if strings.HasPrefix(source, "--") {
		how = source + " was passed"
	}
	err := fmt.Errorf("%s does not support %s: it operates on a local Docker container or local filesystem state with no remote equivalent (%s)", label, source, how)
	sink.Emit(output.ErrorEvent{Title: err.Error()})
	return output.NewSilentError(err)
}
