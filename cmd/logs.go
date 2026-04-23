package cmd

import (
	"fmt"
	"os"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/container"
	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/ui"
	"github.com/spf13/cobra"
)

func newLogsCmd(cfg *env.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "logs",
		Short:   "Show emulator logs",
		Long:    "Show logs from the emulator. Use --follow to stream in real-time.",
		PreRunE: initConfig,
		RunE: func(cmd *cobra.Command, args []string) error {
			follow, err := cmd.Flags().GetBool("follow")
			if err != nil {
				return err
			}
			verbose, err := cmd.Flags().GetBool("verbose")
			if err != nil {
				return err
			}
			rt, err := runtime.NewDockerRuntime(cfg.DockerHost)
			if err != nil {
				return err
			}
			appConfig, err := config.Get()
			if err != nil {
				return fmt.Errorf("failed to get config: %w", err)
			}
			if isInteractiveMode(cfg) {
				return ui.RunLogs(cmd.Context(), rt, appConfig.Containers, follow, verbose)
			}
			return container.Logs(cmd.Context(), rt, output.NewPlainSink(os.Stdout), appConfig.Containers, follow, verbose)
		},
	}
	cmd.Flags().BoolP("follow", "f", false, "Follow log output")
	cmd.Flags().BoolP("verbose", "v", false, "Show all log output without filtering")
	return cmd
}
