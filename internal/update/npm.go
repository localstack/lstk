package update

import (
	"context"
	"os/exec"

	"github.com/localstack/lstk/internal/output"
)

func updateNPM(ctx context.Context, sink output.Sink) error {
	cmd := exec.CommandContext(ctx, "npm", "install", "-g", "@localstack/lstk@latest")
	w := newLogLineWriter(sink, output.LogSourceNPM)
	cmd.Stdout = w
	cmd.Stderr = w
	err := cmd.Run()
	w.Flush()
	return err
}
