package awscli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/localstack/lstk/internal/awsconfig"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/terminal"
)

// stopOnWriteWriter wraps a writer and stops the spinner on first write
type stopOnWriteWriter struct {
	w       io.Writer
	spinner *terminal.Spinner
	once    sync.Once
}

func (s *stopOnWriteWriter) Write(p []byte) (int, error) {
	s.once.Do(func() {
		s.spinner.Stop()
	})
	return s.w.Write(p)
}

func Exec(ctx context.Context, endpointURL string, args []string) error {
	awsBin, err := exec.LookPath("aws")
	if err != nil {
		return fmt.Errorf("aws CLI not found in PATH — install it from https://aws.amazon.com/cli/")
	}

	// Use the localstack AWS profile when it exists (written by lstk start/setup aws),
	// so credentials and region come from ~/.aws rather than being re-injected here.
	// Fall back to env var injection when the profile hasn't been set up yet.
	profileExists, _ := awsconfig.ProfileExists()

	capacity := len(args) + 2
	if profileExists {
		capacity += 2
	}
	cmdArgs := make([]string, 0, capacity)
	cmdArgs = append(cmdArgs, "--endpoint-url", endpointURL)
	if profileExists {
		cmdArgs = append(cmdArgs, "--profile", awsconfig.ProfileName)
	}
	cmdArgs = append(cmdArgs, args...)

	cmd := exec.CommandContext(ctx, awsBin, cmdArgs...)
	cmd.Stdin = os.Stdin
	if !profileExists {
		cmd.Env = BuildEnv(os.Environ())
	}

	var s *terminal.Spinner
	if terminal.IsTerminal(os.Stderr) {
		s = terminal.NewSpinner(os.Stderr, "Loading...")
		s.Start()

		// Wrap stdout/stderr to stop spinner on first output
		stopWriter := &stopOnWriteWriter{w: os.Stdout, spinner: s}
		cmd.Stdout = stopWriter
		cmd.Stderr = &stopOnWriteWriter{w: os.Stderr, spinner: s}
	} else {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	err = cmd.Run()

	if s != nil {
		s.Stop()
	}

	if err == nil {
		return nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return output.NewSilentError(err)
	}
	return err
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
