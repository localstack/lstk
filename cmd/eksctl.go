package cmd

import (
	"fmt"
	"os"

	"github.com/localstack/lstk/internal/eksctl"
	"github.com/localstack/lstk/internal/endpoint"
	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/log"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/spf13/cobra"
)

func newEksctlCmd(cfg *env.Env, logger log.Logger) *cobra.Command {
	// DisableFlagParsing means Cobra won't strip lstk's own flags; PreRunE does
	// that and stashes the remaining args here for RunE to forward to eksctl.
	var passthrough []string
	return &cobra.Command{
		Use:   "eksctl [args...]",
		Short: "Run eksctl against LocalStack",
		Long: `Proxy eksctl commands to the running LocalStack emulator.

Requires eksctl version 0.181.0 or newer on your PATH. lstk points eksctl at LocalStack by setting the AWS service endpoint environment variables it reads (CloudFormation, EC2, EKS, ELB, ELBv2, IAM, STS, plus the generic AWS_ENDPOINT_URL), so cluster operations target the emulator instead of real AWS. This is the "Newer Versions" flow from the LocalStack docs; older eksctl releases are rejected since lstk supports only that flow.

eksctl support in LocalStack is experimental and may not work in all cases.

Supported environment variables:
  LSTK_EKSCTL_CMD    eksctl binary to invoke (default eksctl)
  AWS_ENDPOINT_URL   Overrides the auto-resolved LocalStack endpoint
  AWS_REGION         Deployment region (default us-east-1)
  AWS_ACCESS_KEY_ID  Access key LocalStack derives the account from (default test)

Examples:
  lstk eksctl create cluster --nodes 1
  lstk eksctl get clusters
  lstk eksctl delete cluster --name my-cluster`,
		DisableFlagParsing: true,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			var gf globalFlags
			passthrough, gf = stripGlobalFlags(args)
			if gf.nonInteractive {
				cfg.NonInteractive = true
			}
			if jsonPrecedesCommandName(cmd.CalledAs()) {
				cfg.JSON = true
			}
			if gf.configPath != "" {
				// initConfigDeferCreate reads the "config" flag, so feed the value back to it.
				if err := cmd.Flags().Set("config", gf.configPath); err != nil {
					return err
				}
			}
			return initConfigDeferCreate(nil)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			sink := output.NewPlainSink(os.Stdout)

			// Offline subcommands (version/info/completion) and --help never
			// contact AWS, so they run without Docker or a running emulator.
			if eksctl.IsOffline(passthrough) {
				return eksctl.Run(cmd.Context(), "", sink, logger, passthrough)
			}

			rt, err := runtime.NewDockerRuntime(cfg.DockerHost)
			if err != nil {
				return err
			}

			awsContainer := resolveAWSContainer()

			if err := rt.IsHealthy(cmd.Context()); err != nil {
				rt.EmitUnhealthyError(sink, err)
				return output.NewSilentError(fmt.Errorf("runtime not healthy: %w", err))
			}

			if err := requireRunningAWSEmulator(cmd.Context(), rt, sink, awsContainer, "eksctl"); err != nil {
				return err
			}

			host, _ := endpoint.ResolveHost(cmd.Context(), awsContainer.Port, cfg.LocalStackHost)

			return eksctl.Run(cmd.Context(), "http://"+host, sink, logger, passthrough)
		},
	}
}
