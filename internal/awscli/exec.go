package awscli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/localstack/lstk/internal/awsconfig"
	"github.com/localstack/lstk/internal/output"
)

func Exec(ctx context.Context, endpointURL string, useProfile bool, stdout, stderr io.Writer, args []string) error {
	awsBin, err := exec.LookPath("aws")
	if err != nil {
		return fmt.Errorf("aws CLI not found in PATH — install it from https://aws.amazon.com/cli/")
	}

	capacity := len(args) + 2
	if useProfile {
		capacity += 2
	}
	cmdArgs := make([]string, 0, capacity)
	cmdArgs = append(cmdArgs, "--endpoint-url", endpointURL)
	if useProfile {
		cmdArgs = append(cmdArgs, "--profile", awsconfig.ProfileName)
	}
	cmdArgs = append(cmdArgs, args...)

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
			return output.NewSilentError(err)
		}
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
