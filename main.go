package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/localstack/lstk/cmd"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := cmd.Execute(ctx); err != nil {
		// A child's exit code (a proxied tool, an extension) and the --json
		// exit-code convention are propagated exactly; anything else collapses
		// to 1. See cmd.ExitCode, which telemetry shares so the recorded
		// exit_code matches the real one.
		os.Exit(cmd.ExitCode(err))
	}
}
