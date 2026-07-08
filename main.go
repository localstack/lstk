package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/localstack/lstk/cmd"
	"github.com/localstack/lstk/internal/output"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := cmd.Execute(ctx); err != nil {
		// A proxied tool (aws, terraform, cdk, sam, az, extensions) exited
		// non-zero: propagate its exact code rather than collapsing to 1.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.ExitCode())
		}
		// A JSON-capable command failed after rendering its error envelope to
		// stdout: use the --json exit-code convention (3 CONFIRMATION_REQUIRED,
		// 4 AUTH_REQUIRED, 1 otherwise) attached by wrapCommandsWithJSONEnvelope.
		// errors.As unwraps through the SilentError wrapper to reach it.
		var codeErr *output.ExitCodeError
		if errors.As(err, &codeErr) {
			os.Exit(codeErr.Code)
		}
		os.Exit(1)
	}
}
