package awscli

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/localstack/lstk/internal/awsconfig"
	"github.com/localstack/lstk/internal/output"
)

const InstallURL = "https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html"

var ErrNotInstalled = errors.New("aws CLI not found in PATH")

func CheckInstalled() error {
	if _, err := exec.LookPath("aws"); err != nil {
		return ErrNotInstalled
	}
	return nil
}

// helpFlags are the flags/tokens the aws CLI recognizes as a help request.
// "help" is a bare pseudo-subcommand the aws CLI accepts at any command level
// (`aws help`, `aws s3 help`), equivalent to -h/--help.
var helpFlags = map[string]bool{"-h": true, "--help": true, "help": true}

// IsHelp reports whether args requests the aws CLI's help output. The aws CLI
// answers this without needing a running emulator or resolved endpoint.
func IsHelp(args []string) bool {
	for _, a := range args {
		if helpFlags[a] {
			return true
		}
	}
	return false
}

func Exec(ctx context.Context, endpointURL string, useProfile bool, stdout, stderr io.Writer, args []string) error {
	ctx, span := otel.Tracer("github.com/localstack/lstk/internal/awscli").Start(ctx, "aws cli")
	defer span.End()

	awsBin, err := exec.LookPath("aws")
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return ErrNotInstalled
	}

	capacity := len(args) + 2
	if useProfile {
		capacity += 2
	}
	cmdArgs := make([]string, 0, capacity)
	if endpointURL != "" {
		cmdArgs = append(cmdArgs, "--endpoint-url", endpointURL)
	}
	if useProfile {
		cmdArgs = append(cmdArgs, "--profile", awsconfig.ProfileName)
	}
	cmdArgs = append(cmdArgs, args...)

	span.SetAttributes(
		attribute.StringSlice("aws.args", args),
		attribute.Bool("aws.use_profile", useProfile),
	)

	cmd := exec.CommandContext(ctx, awsBin, cmdArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if !useProfile {
		cmd.Env = BuildEnv(os.Environ())
	}

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			span.SetAttributes(attribute.Int("aws.exit_code", exitErr.ExitCode()))
			span.SetStatus(codes.Error, "aws cli exited non-zero")
			return output.NewSilentError(err)
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	return nil
}

func BuildEnv(base []string) []string {
	env := make([]string, len(base), len(base)+3)
	copy(env, base)

	setIfAbsent(&env, "AWS_ACCESS_KEY_ID", "test")
	setIfAbsent(&env, "AWS_SECRET_ACCESS_KEY", "test")
	setIfAbsent(&env, "AWS_DEFAULT_REGION", "us-east-1")

	return env
}

func setIfAbsent(env *[]string, key, value string) {
	prefix := key + "="
	for _, e := range *env {
		if strings.HasPrefix(e, prefix) {
			return
		}
	}
	*env = append(*env, prefix+value)
}
