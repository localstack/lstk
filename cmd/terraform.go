package cmd

import (
	"fmt"
	"os"

	"github.com/localstack/lstk/internal/endpoint"
	"github.com/localstack/lstk/internal/env"
	tfcli "github.com/localstack/lstk/internal/iac/terraform/cli"
	"github.com/localstack/lstk/internal/log"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/spf13/cobra"
)

func newTerraformCmd(cfg *env.Env, logger log.Logger) *cobra.Command {
	// DisableFlagParsing means Cobra won't strip lstk's own flags; PreRunE does
	// that and stashes the remaining args here for RunE to forward to terraform.
	var passthrough []string
	return &cobra.Command{
		Use:     "terraform [args...]",
		Aliases: []string{"tf"},
		Short:   "Run Terraform against LocalStack",
		Long: `Proxy Terraform commands to LocalStack, using LocalStack endpoints as AWS provider overrides.

lstk-specific flags (must appear before the terraform action):
  --region <region>    Deployment region (default us-east-1)
  --account <id>       Target AWS account id, 12 digits (default test)

Supported environment variables:
  AWS_ENDPOINT_URL            Override the auto-resolved LocalStack endpoint
  LSTK_TF_CMD                 Terraform binary to invoke (e.g. tofu; default terraform)
  LSTK_TF_OVERRIDE_FILE_NAME  Override file name (default localstack_providers_override.tf)
  LSTK_TF_DRY_RUN             Generate the override file but do not run terraform
  AWS_REGION                  Fallback for --region
  AWS_ACCESS_KEY_ID           Fallback for --account

Examples:
  lstk terraform init
  lstk terraform --region us-west-2 plan
  lstk tf apply`,
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

			// --region/--account are only meaningful in leading position after
			// the subcommand. Cobra consumes flags placed before the subcommand
			// during command resolution (silently dropping them), so guard
			// against that explicitly with a clear error rather than a no-op.
			if err := rejectPreSubcommandFlags(cmd.CalledAs()); err != nil {
				return emitValidationError(sink, err)
			}

			tfArgs, regionFlag, accountFlag, chdir, err := stripLeadingIaCFlags(passthrough, true)
			if err != nil {
				return emitValidationError(sink, err)
			}

			region := resolveRegion(regionFlag)
			account, err := resolveAccount(accountFlag)
			if err != nil {
				return emitValidationError(sink, err)
			}

			workdir, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("resolving working directory: %w", err)
			}
			// -chdir switches terraform into another directory, so the S3-backend
			// detection that decides whether an emulator is required must inspect
			// that directory too (matching the resolution Run does internally).
			if chdir != "" {
				workdir = tfcli.ResolveChdir(workdir, chdir)
			}

			// Commands that don't need the emulator (fmt/validate/version, and
			// init when no S3 backend is declared) run without bringing up or
			// requiring a running emulator.
			if !tfcli.RequiresEmulator(tfArgs, workdir, logger) {
				return tfcli.Run(cmd.Context(), "", region, account, chdir, sink, logger, tfArgs)
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

			if err := requireRunningAWSEmulator(cmd.Context(), rt, sink, awsContainer, "terraform"); err != nil {
				return err
			}

			host, _ := endpoint.ResolveHost(cmd.Context(), awsContainer.Port, cfg.LocalStackHost)

			return tfcli.Run(cmd.Context(), "http://"+host, region, account, chdir, sink, logger, tfArgs)
		},
	}
}
