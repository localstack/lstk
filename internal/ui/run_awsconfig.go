package ui

import (
	"context"

	"github.com/localstack/lstk/internal/awsconfig"
	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/endpoint"
	"github.com/localstack/lstk/internal/output"
)

// RunSetupAWS runs the AWS profile setup flow with TUI output.
// It resolves the host from the AWS container config and runs the setup.
// When force is true, the confirmation prompt is skipped.
func RunSetupAWS(parentCtx context.Context, containers []config.ContainerConfig, localStackHost string, force bool) error {
	resolvedHost, dnsOK, err := awsconfig.ResolveProfileHost(parentCtx, containers, localStackHost)
	if err != nil {
		return err
	}

	return runWithTUI(parentCtx, withoutHeader(), func(ctx context.Context, sink output.Sink) error {
		if !dnsOK {
			sink.Emit(output.MessageEvent{Severity: output.SeverityNote, Text: endpoint.DNSRebindNote})
		}
		status, err := awsconfig.CheckProfileStatus(resolvedHost)
		if err != nil {
			return err
		}
		return awsconfig.Setup(ctx, sink, resolvedHost, status, force, true)
	})
}
