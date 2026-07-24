package cmd

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/container"
	tfcli "github.com/localstack/lstk/internal/iac/terraform/cli"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
)

// Shared command-boundary helpers for the IaC proxy commands (terraform, cdk).
// These live here rather than in any one command's file because both commands
// depend on them equally; keeping them in cmd/ (not a domain package) is
// deliberate — they touch config.Get(), the output.Sink, and the raw CLI args,
// all of which are command-boundary concerns.

var accountIDRe = regexp.MustCompile(`^\d{12}$`)

// requireRunningAWSEmulator verifies the AWS emulator is reachable before an
// IaC proxy command (terraform/cdk/sam) that contacts AWS proceeds — a managed
// container found via Docker, or an already-running instance answering the
// HTTP probe on host (e.g. LocalStack running from source). When nothing is
// reachable it emits an actionable error through the sink — an AWS-specific
// message naming the other emulator when a non-AWS one is up, otherwise the
// generic "not running" error — and returns a silent error. cmdLabel is the
// lstk command name used in the message (e.g. "terraform"/"cdk"). It returns nil
// when the AWS emulator is reachable.
func requireRunningAWSEmulator(ctx context.Context, dockerHost string, sink output.Sink, awsContainer config.ContainerConfig, host, cmdLabel string) error {
	resolved, rt, err := resolveReachableEmulator(ctx, dockerHost, sink, awsContainer, host)
	if err != nil {
		return err
	}
	if resolved.Found() {
		return nil
	}
	// These commands only work with the AWS emulator. If a different emulator
	// is running, say so specifically rather than reporting a misleading
	// "AWS not running". Skipped when Docker is unavailable (rt == nil).
	if rt != nil {
		if other := runningNonAWSEmulator(ctx, rt); other != "" {
			sink.Emit(output.ErrorEvent{
				Title: fmt.Sprintf("lstk %s requires the %s, but the %s is running", cmdLabel, awsContainer.DisplayName(), other),
				Actions: []output.ErrorAction{
					{Label: "Start the AWS emulator:", Value: "lstk"},
				},
			})
			return output.NewSilentError(fmt.Errorf("lstk %s requires the AWS emulator, but the %s is running", cmdLabel, other))
		}
	}
	return container.HandleNoRunningContainer(sink, awsContainer)
}

// runningNonAWSEmulator returns the display name of a running non-AWS emulator
// (e.g. Snowflake or Azure), or "" if none is running. The IaC proxy commands
// support only the AWS emulator, so this lets them give a specific error when a
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
// sink (consistent with the other IaC proxy error events) and returns a silent
// error so the top-level handler does not print it a second time.
func emitValidationError(sink output.Sink, err error) error {
	sink.Emit(output.ErrorEvent{Title: err.Error()})
	return output.NewSilentError(err)
}

// rejectPreSubcommandFlags returns an error if --region or --account appears in
// the raw command line before the subcommand token. Such flags are consumed by
// Cobra during command resolution and would otherwise be silently dropped;
// calledAs is the name the command was invoked as (e.g. "terraform"/"tf"/"cdk").
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
			return fmt.Errorf("--region and --account must appear after the %s subcommand (e.g. `lstk %s --region us-west-2 ...`)", calledAs, calledAs)
		}
	}
	return nil
}

// stripLeadingIaCFlags extracts the lstk-specific --region/--account flags, and
// (when recognizeChdir is set) reads terraform's global -chdir, but only in
// leading position (between the subcommand alias and the action). It accepts
// both --flag value and --flag=value forms for the lstk flags and -chdir=DIR for
// chdir, stops at the first token that is none of these (forwarding the rest
// verbatim), and errors if a leading lstk flag is missing its value.
//
// --region/--account are consumed and removed from the returned args; -chdir is
// read for lstk's own working-directory resolution but kept in the returned
// args, because terraform itself must also see it to switch directories. Only
// the -chdir=DIR form is recognized (terraform does not accept a space-separated
// -chdir DIR); any other spelling falls through and is forwarded verbatim. CDK
// has no -chdir equivalent, so it calls this with recognizeChdir=false and
// ignores the returned chdir.
func stripLeadingIaCFlags(args []string, recognizeChdir bool) (remaining []string, region, account, chdir string, err error) {
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
		case recognizeChdir && strings.HasPrefix(arg, "-chdir="):
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
