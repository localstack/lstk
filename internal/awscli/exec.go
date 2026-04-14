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

	"github.com/localstack/lstk/internal/output"
)

// stopOnWriteWriter wraps a writer and stops the spinner on first write
type stopOnWriteWriter struct {
	w       io.Writer
	spinner *spinner
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

	proxyURL, stopProxy := startTraceProxy(ctx, endpointURL)
	defer stopProxy()

	cmdArgs := make([]string, 0, len(args)+2)
	cmdArgs = append(cmdArgs, "--endpoint-url", proxyURL)
	cmdArgs = append(cmdArgs, args...)

	cmd := exec.CommandContext(ctx, awsBin, cmdArgs...)
	cmd.Stdin = os.Stdin
	cmd.Env = BuildEnv(os.Environ())

	var s *spinner
	if isTerminal(os.Stderr) {
		s = newSpinner(os.Stderr, "Loading...")
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

	if err == nil {
		return nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return output.NewSilentError(output.NewExitCodeError(exitErr.ExitCode(), err))
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
