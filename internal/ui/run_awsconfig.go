package ui

import (
	"context"
	"fmt"

	"github.com/localstack/lstk/internal/awsconfig"
	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/endpoint"
	"github.com/localstack/lstk/internal/output"
)

// RunConfigProfile runs the AWS profile setup flow with TUI output.
// It resolves the host from the AWS container config and runs the setup.
func RunConfigProfile(parentCtx context.Context, containers []config.ContainerConfig, localStackHost string) error {
	awsContainer := config.GetAWSContainer(containers)
	if awsContainer == nil {
		return fmt.Errorf("no aws emulator configured")
	}

	resolvedHost, dnsOK := endpoint.ResolveHost(awsContainer.Port, localStackHost)

	return runWithTUI(parentCtx, withoutHeader(), func(ctx context.Context, sink output.Sink) error {
		if !dnsOK {
			output.EmitNote(sink, `Could not resolve "localhost.localstack.cloud" - your system may have DNS rebind protection enabled. Using 127.0.0.1 as the endpoint.`)
		}
		status, err := awsconfig.CheckProfileStatus(resolvedHost)
		if err != nil {
			return err
		}
		return awsconfig.Setup(ctx, sink, resolvedHost, status)
	})
}

