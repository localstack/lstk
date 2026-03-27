package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/localstack/lstk/cmd"
	"github.com/localstack/lstk/internal/output"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := cmd.Execute(ctx); err != nil {
		os.Exit(output.ExitCode(err))
	}
}
