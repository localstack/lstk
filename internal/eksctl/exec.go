// Package eksctl is the exec wrapper behind the `lstk eksctl` proxy command. It
// runs the eksctl binary against the running LocalStack emulator by setting the
// AWS service endpoint environment variables it reads (see env.go), mirroring
// the "Newer Versions" flow documented at
// https://docs.localstack.cloud/aws/customization/kubernetes/eksctl/.
package eksctl

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/localstack/lstk/internal/log"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/proc"
)

const installDocsURL = "https://eksctl.io/installation/"

// ErrNotInstalled is returned when the eksctl binary cannot be found in PATH.
var ErrNotInstalled = errors.New("eksctl not found in PATH")

// eksctlCmd returns the eksctl binary name to invoke, honoring LSTK_EKSCTL_CMD
// and defaulting to "eksctl".
func eksctlCmd() string {
	if v := os.Getenv("LSTK_EKSCTL_CMD"); v != "" {
		return v
	}
	return "eksctl"
}

func isOfflineCommand(command string) bool {
	switch command {
	case "version", "info", "help", "completion":
		return true
	default:
		return false
	}
}

// IsHelp reports whether args requests eksctl's help output. eksctl answers this
// without needing a running emulator.
func IsHelp(args []string) bool {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			return true
		}
		flag, value, hasValue := strings.Cut(a, "=")
		if !hasValue || (flag != "-h" && flag != "--help") {
			continue
		}
		enabled, err := strconv.ParseBool(value)
		if err != nil || enabled {
			return true
		}
	}
	return false
}

// IsOffline reports whether the eksctl invocation described by args is one of the
// subcommands that need no running emulator (or a help request).
func IsOffline(args []string) bool {
	return IsHelp(args) || isOfflineCommand(subcommand(args))
}

func globalFlagTakesValue(flag string) bool {
	switch flag {
	case "-v", "--verbose", "-C", "--color":
		return true
	default:
		return false
	}
}

// subcommand returns the first non-flag token in args that is not consumed as a
// global option's value, or "" if there is none.
func subcommand(args []string) string {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if len(a) == 0 {
			continue
		}
		if a[0] == '-' {
			if globalFlagTakesValue(a) && i+1 < len(args) {
				i++ // skip this flag's value
			}
			continue
		}
		return a
	}
	return ""
}

// Run proxies an eksctl invocation against LocalStack. It locates the eksctl
// binary, verifies its version (unless the subcommand is offline), builds a
// subprocess environment that points eksctl at the resolved LocalStack endpoint,
// then runs eksctl with stdio wired through.
//
// endpointURL is the resolved LocalStack endpoint (http://host:port), or "" for
// offline subcommands that do not contact AWS; a user-set AWS_ENDPOINT_URL
// takes precedence over the resolved endpoint (same contract as the
// terraform/cdk/sam proxies). eksctl output is streamed unobstructed (no
// spinner); a non-zero exit is wrapped as a silent error so lstk does not
// reprint it.
func Run(ctx context.Context, endpointURL string, sink output.Sink, logger log.Logger, args []string) error {
	ctx, span := otel.Tracer("github.com/localstack/lstk/internal/eksctl").Start(ctx, "eksctl cli")
	defer span.End()

	bin, err := exec.LookPath(eksctlCmd())
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		sink.Emit(output.ErrorEvent{
			Title:   fmt.Sprintf("%s not found in PATH", eksctlCmd()),
			Actions: []output.ErrorAction{{Label: "Install eksctl:", Value: installDocsURL}},
		})
		return output.NewSilentError(ErrNotInstalled)
	}

	offline := IsOffline(args)
	if !offline {
		if err := CheckVersion(ctx, bin); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			sink.Emit(output.ErrorEvent{
				Title:   err.Error(),
				Actions: []output.ErrorAction{{Label: "Upgrade eksctl:", Value: installDocsURL}},
			})
			return output.NewSilentError(err)
		}
	}

	if !offline {
		if override := endpointURLOverride(); override != "" {
			endpointURL = override
			logger.Info("eksctl: using AWS_ENDPOINT_URL override %s", override)
		}
	}

	span.SetAttributes(
		attribute.StringSlice("eksctl.args", args),
		attribute.Bool("eksctl.offline", offline),
	)

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = BuildEnv(os.Environ(), endpointURL)

	logger.Info("eksctl: running %s (offline=%t)", bin, offline)

	if err := proc.Run(cmd); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			span.SetAttributes(attribute.Int("eksctl.exit_code", exitErr.ExitCode()))
			span.SetStatus(codes.Error, "eksctl exited non-zero")
			return output.NewSilentError(err)
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	return nil
}
