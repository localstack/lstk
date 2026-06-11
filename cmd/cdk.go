package cmd

import (
	"fmt"
	"os"

	"github.com/localstack/lstk/internal/endpoint"
	"github.com/localstack/lstk/internal/env"
	cdkcli "github.com/localstack/lstk/internal/iac/cdk/cli"
	"github.com/localstack/lstk/internal/log"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/spf13/cobra"
)

func newCDKCmd(cfg *env.Env, logger log.Logger) *cobra.Command {
	// DisableFlagParsing means Cobra won't strip lstk's own flags; PreRunE does
	// that and stashes the remaining args here for RunE to forward to cdk.
	var passthrough []string
	return &cobra.Command{
		Use:   "cdk [args...]",
		Short: "Run AWS CDK against LocalStack",
		Long: `Proxy AWS CDK commands to the real cdk CLI, so deploys target the running emulator instead of real AWS.

Requires the AWS CDK CLI version 2.177.0 or newer on your PATH (lstk targets LocalStack purely through environment variables, which older CDK versions ignore). LocalStack must be running for commands that contact AWS (bootstrap, deploy, destroy, diff, …); offline commands (init, synth, ls, version, doctor) run without it.

lstk-specific flags (must appear before the cdk action):
  --region <region>    Deployment region (default us-east-1)
  --account <id>       Target AWS account id, 12 digits (default test)

Supported environment variables:
  AWS_ENDPOINT_URL      Override the auto-resolved LocalStack endpoint
  AWS_ENDPOINT_URL_S3   Override the auto-derived S3 endpoint
  LSTK_CDK_CMD          CDK binary to invoke (default cdk)
  AWS_REGION            Fallback for --region
  AWS_ACCESS_KEY_ID     Fallback for --account

Examples:
  lstk cdk bootstrap
  lstk cdk --region us-west-2 deploy
  lstk cdk synth`,
		DisableFlagParsing: true,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			var gf globalFlags
			passthrough, gf = stripGlobalFlags(args)
			if gf.nonInteractive {
				cfg.NonInteractive = true
			}
			if gf.configPath != "" {
				if err := cmd.Flags().Set("config", gf.configPath); err != nil {
					return err
				}
			}
			return initConfig(nil)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			sink := output.NewPlainSink(os.Stdout)

			if err := rejectPreSubcommandFlags(cmd.CalledAs()); err != nil {
				return emitValidationError(sink, err)
			}

			cdkArgs, regionFlag, accountFlag, _, err := stripLeadingIaCFlags(passthrough, false)
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
			// inject it, so a synth-time context lookup routes to LocalStack.
			if cdkcli.IsOffline(cdkArgs) {
				host, _ := endpoint.ResolveHost(cmd.Context(), awsContainer.Port, cfg.LocalStackHost)
				return cdkcli.Run(cmd.Context(), "http://"+host, region, account, sink, logger, cdkArgs)
			}

			rt, err := runtime.NewDockerRuntime(cfg.DockerHost)
			if err != nil {
				return err
			}

			if err := rt.IsHealthy(cmd.Context()); err != nil {
				rt.EmitUnhealthyError(sink, err)
				return output.NewSilentError(fmt.Errorf("runtime not healthy: %w", err))
			}

			if err := requireRunningAWSEmulator(cmd.Context(), rt, sink, awsContainer, "cdk"); err != nil {
				return err
			}

			host, dnsOK := endpoint.ResolveHost(cmd.Context(), awsContainer.Port, cfg.LocalStackHost)
			if !dnsOK {
				// CDK has no env-only lever to force S3 path style, so on the
				// loopback fallback its S3 asset operations (bootstrap, asset
				// deploys) may fail virtual-host addressing. Warn rather than
				// block — non-S3 services still work. See the cdk-proxy design.
				sink.Emit(output.MessageEvent{
					Severity: output.SeverityWarning,
					Text:     "Could not resolve localhost.localstack.cloud; using 127.0.0.1. CDK S3 asset operations (bootstrap, asset deploys) may fail on this host — ensure localhost.localstack.cloud resolves, or set AWS_ENDPOINT_URL/AWS_ENDPOINT_URL_S3 to a virtual-host-capable host.",
				})
			}

			return cdkcli.Run(cmd.Context(), "http://"+host, region, account, sink, logger, cdkArgs)
		},
	}
}
