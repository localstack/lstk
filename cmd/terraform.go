package cmd

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/container"
	"github.com/localstack/lstk/internal/endpoint"
	"github.com/localstack/lstk/internal/env"
	tfcli "github.com/localstack/lstk/internal/iac/terraform/cli"
	"github.com/localstack/lstk/internal/log"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/spf13/cobra"
)

var accountIDRe = regexp.MustCompile(`^\d{12}$`)

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
			if gf.configPath != "" {
				if err := cmd.Flags().Set("config", gf.configPath); err != nil {
					return err
				}
			}
			return initConfig(nil)(cmd, args)
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

			tfArgs, regionFlag, accountFlag, chdir, err := stripLeadingTerraformFlags(passthrough)
			if err != nil {
				return emitValidationError(sink, err)
			}

			region := resolveRegion(regionFlag)
			account, err := resolveAccount(accountFlag)
			if err != nil {
				return emitValidationError(sink, err)
			}

			// Unproxied subcommands (fmt/validate/version) never touch the
			// endpoint, so they run without requiring a running emulator.
			if tfcli.IsUnproxied(tfArgs) {
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

			runningName, err := container.ResolveRunningContainerName(cmd.Context(), rt, awsContainer)
			if err != nil {
				return fmt.Errorf("checking emulator status: %w", err)
			}
			if runningName == "" {
				// lstk terraform only works with the AWS emulator. If a
				// different emulator is running, say so specifically rather than
				// reporting a misleading "AWS not running".
				if other := runningNonAWSEmulator(cmd.Context(), rt); other != "" {
					sink.Emit(output.ErrorEvent{
						Title: fmt.Sprintf("lstk terraform requires the %s, but the %s is running", awsContainer.DisplayName(), other),
						Actions: []output.ErrorAction{
							{Label: "Start the AWS emulator:", Value: "lstk"},
						},
					})
					return output.NewSilentError(fmt.Errorf("lstk terraform requires the AWS emulator, but the %s is running", other))
				}
				sink.Emit(output.ErrorEvent{
					Title: fmt.Sprintf("%s is not running", awsContainer.DisplayName()),
					Actions: []output.ErrorAction{
						{Label: "Start LocalStack:", Value: "lstk"},
						{Label: "See help:", Value: "lstk -h"},
					},
				})
				return output.NewSilentError(fmt.Errorf("%s is not running", awsContainer.Name()))
			}

			host, _ := endpoint.ResolveHost(cmd.Context(), awsContainer.Port, cfg.LocalStackHost)

			return tfcli.Run(cmd.Context(), "http://"+host, region, account, chdir, sink, logger, tfArgs)
		},
	}
}

// runningNonAWSEmulator returns the display name of a running non-AWS emulator
// (e.g. Snowflake or Azure), or "" if none is running. lstk terraform supports
// only the AWS emulator, so this lets the command give a specific error when a
// different emulator is running instead of a misleading "AWS not running".
func runningNonAWSEmulator(ctx context.Context, rt runtime.Runtime) string {
	var others []config.ContainerConfig
	for _, t := range config.SelectableEmulatorTypes {
		if t == config.EmulatorAWS {
			continue
		}
		others = append(others, config.ContainerConfig{Type: t, Port: config.DefaultPort})
	}
	running, err := container.RunningEmulators(ctx, rt, others)
	if err != nil || len(running) == 0 {
		return ""
	}
	return running[0].DisplayName()
}

// resolveAWSContainer returns the configured AWS emulator container, falling
// back to defaults when no matching entry exists (mirrors cmd/aws.go).
func resolveAWSContainer() config.ContainerConfig {
	awsContainer := config.ContainerConfig{Type: config.EmulatorAWS, Port: config.DefaultPort}
	appCfg, err := config.Get()
	if err != nil {
		return awsContainer
	}
	for _, c := range appCfg.Containers {
		if c.Type == config.EmulatorAWS {
			return c
		}
	}
	return awsContainer
}

// emitValidationError renders a command-boundary validation failure through the
// sink (consistent with the other terraform error events) and returns a silent
// error so the top-level handler does not print it a second time.
func emitValidationError(sink output.Sink, err error) error {
	sink.Emit(output.ErrorEvent{Title: err.Error()})
	return output.NewSilentError(err)
}

// rejectPreSubcommandFlags returns an error if --region or --account appears in
// the raw command line before the terraform/tf subcommand token. Such flags are
// consumed by Cobra during command resolution and would otherwise be silently
// dropped; calledAs is the name the command was invoked as ("terraform"/"tf").
func rejectPreSubcommandFlags(calledAs string) error {
	cmdIdx := -1
	for i, a := range os.Args {
		if a == calledAs {
			cmdIdx = i
			break
		}
	}
	if cmdIdx <= 0 {
		return nil
	}
	for _, a := range os.Args[1:cmdIdx] {
		if a == "--region" || a == "--account" ||
			strings.HasPrefix(a, "--region=") || strings.HasPrefix(a, "--account=") {
			return fmt.Errorf("--region and --account must appear after the terraform subcommand (e.g. `lstk terraform --region us-west-2 plan`)")
		}
	}
	return nil
}

// stripLeadingTerraformFlags extracts the lstk-specific --region/--account
// flags and reads terraform's global -chdir, but only in leading position
// (between terraform/tf and the action). It accepts both --flag value and
// --flag=value forms for the lstk flags and -chdir=DIR for chdir, stops at the
// first token that is none of these (forwarding the rest verbatim), and errors
// if a leading lstk flag is missing its value.
//
// --region/--account are consumed and removed from the returned args; -chdir is
// read for lstk's own working-directory resolution but kept in the returned
// args, because terraform itself must also see it to switch directories. Only
// the -chdir=DIR form is recognized (terraform does not accept a space-separated
// -chdir DIR); any other spelling falls through and is forwarded verbatim for
// terraform to reject.
func stripLeadingTerraformFlags(args []string) (remaining []string, region, account, chdir string, err error) {
	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "--region":
			if i+1 >= len(args) {
				return nil, "", "", "", fmt.Errorf("--region requires a value")
			}
			region = args[i+1]
			i += 2
		case strings.HasPrefix(arg, "--region="):
			region = strings.TrimPrefix(arg, "--region=")
			i++
		case arg == "--account":
			if i+1 >= len(args) {
				return nil, "", "", "", fmt.Errorf("--account requires a value")
			}
			account = args[i+1]
			i += 2
		case strings.HasPrefix(arg, "--account="):
			account = strings.TrimPrefix(arg, "--account=")
			i++
		case strings.HasPrefix(arg, "-chdir="):
			// Read the value but keep -chdir in the forwarded args so terraform
			// also switches into it; continue scanning so leading --region/--account
			// positioned after -chdir are still consumed.
			chdir = strings.TrimPrefix(arg, "-chdir=")
			remaining = append(remaining, arg)
			i++
		default:
			return append(remaining, args[i:]...), region, account, chdir, nil
		}
	}
	return remaining, region, account, chdir, nil
}

// resolveRegion applies the precedence --region flag → AWS_REGION → us-east-1.
// The deprecated AWS_DEFAULT_REGION is intentionally not consulted.
func resolveRegion(flag string) string {
	if flag != "" {
		return flag
	}
	if v := os.Getenv("AWS_REGION"); v != "" {
		return v
	}
	return "us-east-1"
}

// resolveAccount applies the precedence --account flag → AWS_ACCESS_KEY_ID →
// test. A flag value must be exactly 12 digits. An AWS_ACCESS_KEY_ID value is
// run through DeactivateAccessKey so a real key (AKIA…/ASIA…) accidentally
// present in the environment is never written into the override or sent to
// LocalStack; the validated 12-digit flag is used as-is.
func resolveAccount(flag string) (string, error) {
	if flag != "" {
		if !accountIDRe.MatchString(flag) {
			return "", fmt.Errorf("--account must be a 12-digit AWS account id, got %q", flag)
		}
		return flag, nil
	}
	if v := os.Getenv("AWS_ACCESS_KEY_ID"); v != "" {
		return tfcli.DeactivateAccessKey(v), nil
	}
	return "test", nil
}
