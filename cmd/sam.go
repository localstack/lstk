package cmd

import (
	"fmt"
	"os"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/endpoint"
	"github.com/localstack/lstk/internal/env"
	samcli "github.com/localstack/lstk/internal/iac/sam/cli"
	"github.com/localstack/lstk/internal/log"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
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

When --region is given, it is also passed to sam as '--region' for the subcommands that contact AWS, because a 'region' key in samconfig.toml outranks the environment and would otherwise silently win. Without --region, samconfig.toml keeps deciding the region as it always has.

Supported environment variables:
  LSTK_ENDPOINT_URL     Target an externally-managed emulator
  AWS_ENDPOINT_URL      Same as LSTK_ENDPOINT_URL (lower precedence if both are set)
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
			// --endpoint-url is recognized only when it precedes "sam", the
			// same pre-command-only placement --json already gets here.
			if strippedArgs, v, ok := stripPreCommandEndpointURL(cmd.CalledAs()); ok {
				if err := cmd.Flags().Set(endpoint.FlagName, v); err != nil {
					return err
				}
				args = strippedArgs
			}

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

			if err := rejectPreSubcommandFlags(cmd.CalledAs(), "--region", "--account"); err != nil {
				return emitValidationError(sink, err)
			}

			samArgs, regionFlag, accountFlag, _, err := stripLeadingProxyFlags(passthrough, leadingFlags{account: true, region: true})
			if err != nil {
				return emitValidationError(sink, err)
			}

			region, regionSelected := resolveRegionSelection(regionFlag)
			account, err := resolveAccount(accountFlag)
			if err != nil {
				return emitValidationError(sink, err)
			}

			target, err := endpoint.Resolve(cmd.Context(), cmd)
			if err != nil {
				return emitValidationError(sink, err)
			}
			if target != nil && target.Type != config.EmulatorAWS {
				return emitValidationError(sink, fmt.Errorf("lstk sam requires the AWS emulator, but the endpoint at %s is a %s emulator", target.URL, target.Type.DisplayName()))
			}

			awsContainer := resolveAWSContainer()

			// Offline subcommands never contact AWS, so they run without a
			// running emulator. We still resolve the endpoint (DNS only, or the
			// externally-managed target) and inject it, so any incidental API
			// call routes to LocalStack.
			if samcli.IsOffline(samArgs) {
				endpointURL := ""
				if target != nil {
					endpointURL = target.URL
				} else {
					host, _ := endpoint.ResolveHost(cmd.Context(), awsContainer.Port, cfg.LocalStackHost)
					endpointURL = "http://" + host
				}
				return samcli.Run(cmd.Context(), endpointURL, account, region, regionSelected, sink, logger, samArgs)
			}

			if target != nil {
				return samcli.Run(cmd.Context(), target.URL, account, region, regionSelected, sink, logger, samArgs)
			}

			rt, err := runtime.NewDockerRuntime(cfg.DockerHost)
			if err != nil {
				return err
			}

			if err := rt.IsHealthy(cmd.Context()); err != nil {
				rt.EmitUnhealthyError(sink, err)
				return output.NewSilentError(fmt.Errorf("runtime not healthy: %w", err))
			}

			if err := requireRunningAWSEmulator(cmd.Context(), rt, sink, awsContainer, "sam"); err != nil {
				return err
			}

			host, _ := endpoint.ResolveHost(cmd.Context(), awsContainer.Port, cfg.LocalStackHost)

			return samcli.Run(cmd.Context(), "http://"+host, account, region, regionSelected, sink, logger, samArgs)
		},
	}
}
