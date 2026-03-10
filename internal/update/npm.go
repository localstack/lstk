package update

import (
	"context"
	"os/exec"

	"github.com/localstack/lstk/internal/output"
)

func updateNPM(ctx context.Context, sink output.Sink, projectDir string) error {
	var cmd *exec.Cmd
	if projectDir != "" {
		cmd = exec.CommandContext(ctx, "npm", "install", "@localstack/lstk")
		cmd.Dir = projectDir
	} else {
		cmd = exec.CommandContext(ctx, "npm", "install", "-g", "@localstack/lstk")
	}
	w := newLogLineWriter(sink, "npm")
	cmd.Stdout = w
	cmd.Stderr = w
	err := cmd.Run()
	w.Flush()
	return err
}
