package update

import (
	"context"
	"os/exec"

	"github.com/localstack/lstk/internal/output"
)

func updateHomebrew(ctx context.Context, sink output.Sink) error {
	cmd := exec.CommandContext(ctx, "brew", "upgrade", "localstack/tap/lstk")
	w := newLogLineWriter(sink, output.LogSourceBrew)
	cmd.Stdout = w
	cmd.Stderr = w
	err := cmd.Run()
	w.Flush()
	return err
}
