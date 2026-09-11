package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/container"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
)

// Shared command-boundary helpers for the IaC proxy commands (terraform, cdk,
// sam). These live here rather than in any one command's file because all three
// depend on them equally; keeping them in cmd/ (not a domain package) is
// deliberate — they touch config.Get(), the output.Sink, and the raw CLI args,
// all of which are command-boundary concerns.
//
// The leading-flag parsing and account resolution that used to live here now
// sit in proxy.go: `lstk aws` shares them, so they are no longer IaC-specific.

// requireRunningAWSEmulator verifies the AWS emulator is running before an IaC
// proxy command (terraform/cdk) that contacts AWS proceeds. When it is not
// running it emits an actionable error through the sink — an AWS-specific
// message naming the other emulator when a non-AWS one is up, otherwise the
// generic "not running" error — and returns a silent error. cmdLabel is the
// lstk command name used in the message (e.g. "terraform"/"cdk"). It returns nil
// when the AWS emulator is running.
func requireRunningAWSEmulator(ctx context.Context, rt runtime.Runtime, sink output.Sink, awsContainer config.ContainerConfig, cmdLabel string) error {
	runningName, err := container.ResolveRunningContainerName(ctx, rt, awsContainer)
	if err != nil {
		return fmt.Errorf("checking emulator status: %w", err)
	}
	if runningName != "" {
		return nil
	}
	// These commands only work with the AWS emulator. If a different emulator
	// is running, say so specifically rather than reporting a misleading
	// "AWS not running".
	if other := runningNonAWSEmulator(ctx, rt); other != "" {
		sink.Emit(output.ErrorEvent{
			Title: fmt.Sprintf("lstk %s requires the %s, but the %s is running", cmdLabel, awsContainer.DisplayName(), other),
			Actions: []output.ErrorAction{
				{Label: "Start the AWS emulator:", Value: "lstk"},
			},
		})
		return output.NewSilentError(fmt.Errorf("lstk %s requires the AWS emulator, but the %s is running", cmdLabel, other))
	}
	return container.HandleNoRunningContainer(sink, awsContainer)
}

// runningNonAWSEmulator returns the display name of a running non-AWS emulator
// (e.g. Snowflake or Azure), or "" if none is running. The IaC proxy commands
// support only the AWS emulator, so this lets them give a specific error when a
// different emulator is running instead of a misleading "AWS not running".
//
// It enumerates every known type, not just the selectable ones: the question is
// what might be running, and a preview emulator the picker never offers can be
// running just as well.
func runningNonAWSEmulator(ctx context.Context, rt runtime.Runtime) string {
	var others []config.ContainerConfig
	for _, t := range config.KnownEmulatorTypes() {
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

// resolveRegionSelection applies the precedence --region flag → AWS_REGION →
// us-east-1, and reports whether the region was named by the flag rather than
// inherited or defaulted.
//
// Only the flag counts as a selection. `lstk sam` uses the signal to decide
// whether to put --region on sam's own command line, which is the only way to
// outrank a region in samconfig.toml; treating an ambient AWS_REGION as a
// selection would start overriding samconfig.toml for the many developers who
// export it globally for real-AWS work, and defaulting to us-east-1 would
// override it for everyone. See withRegionFlag in internal/iac/sam/cli.
func resolveRegionSelection(flag string) (region string, selected bool) {
	if flag != "" {
		return flag, true
	}
	if v := os.Getenv("AWS_REGION"); v != "" {
		return v, false
	}
	return "us-east-1", false
}

// resolveRegion is resolveRegionSelection for callers that encode the region
// into their own configuration and do not care how it was chosen (terraform,
// cdk). The deprecated AWS_DEFAULT_REGION is intentionally not consulted.
func resolveRegion(flag string) string {
	region, _ := resolveRegionSelection(flag)
	return region
}
