package cmd

import (
	"os"

	"github.com/localstack/lstk/internal/endpoint"
	"github.com/localstack/lstk/internal/env"
	samcli "github.com/localstack/lstk/internal/iac/sam/cli"
	"github.com/localstack/lstk/internal/log"
	"github.com/localstack/lstk/internal/output"
	"github.com/spf13/cobra"
)

func newSamCmd(cfg *env.Env, logger log.Logger) *cobra.Command {
	// DisableFlagParsing means Cobra won't strip lstk's own flags; PreRunE does
	// that and stashes the remaining args here for RunE to forward to sam.
	var passthrough []string
	return &cobra.Command{
		Use:   "sam [args...]",
		Short: "Run the AWS SAM CLI against LocalStack",
		Long: `Proxy AWS SAM CLI commands to the running LocalStack emulator.

Requires the AWS SAM CLI version 1.95.0 or newer on your PATH (older versions ignore AWS_ENDPOINT_URL and would target real AWS).

lstk-specific flags (must appear before the sam action):
  --region <region>    Deployment region (default us-east-1)
  --account <id>       Target AWS account id, 12 digits (default 000000000000)

Supported environment variables:
  AWS_ENDPOINT_URL      Override the auto-resolved LocalStack endpoint
  AWS_ENDPOINT_URL_S3   Override S3 endpoint
  LSTK_SAM_CMD          SAM binary to invoke (default sam)
  AWS_REGION            Fallback for --region
  AWS_ACCESS_KEY_ID     Fallback for --account

Known limitations versus samlocal: image/container-based Lambda (ECR) deploys and nested CloudFormation stacks are not supported; use samlocal for those workflows.

Examples:
  lstk sam build
  lstk sam --region us-west-2 deploy
  lstk sam validate`,
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
				if err := cmd.Flags().Set("config", gf.configPath); err != nil {
					return err
				}
			}
			return initConfigDeferCreate(nil)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			sink := output.NewPlainSink(os.Stdout)

			if err := rejectPreSubcommandFlags(cmd.CalledAs()); err != nil {
				return emitValidationError(sink, err)
			}

			samArgs, regionFlag, accountFlag, _, err := stripLeadingIaCFlags(passthrough, false)
			if err != nil {
				return emitValidationError(sink, err)
			}

			region := resolveRegion(regionFlag)
			account, err := resolveAccount(accountFlag)
			if err != nil {
				return emitValidationError(sink, err)
			}

			awsContainer := resolveAWSContainer()

			// Offline subcommands never contact AWS, so they run without a
			// running emulator. We still resolve the endpoint (DNS only) and
			// inject it, so any incidental API call routes to LocalStack.
			if samcli.IsOffline(samArgs) {
				host, _ := endpoint.ResolveHost(cmd.Context(), awsContainer.Port, cfg.LocalStackHost)
				return samcli.Run(cmd.Context(), "http://"+host, account, region, sink, logger, samArgs)
			}

			host, _ := endpoint.ResolveHost(cmd.Context(), awsContainer.Port, cfg.LocalStackHost)

			if err := requireRunningAWSEmulator(cmd.Context(), cfg.DockerHost, sink, awsContainer, host, "sam"); err != nil {
				return err
			}

			return samcli.Run(cmd.Context(), "http://"+host, account, region, sink, logger, samArgs)
		},
	}
}
