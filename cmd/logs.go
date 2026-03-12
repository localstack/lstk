package cmd

import (
	"fmt"
	"os"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/container"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/spf13/cobra"
)

func newLogsCmd() *cobra.Command {
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
			rt, err := runtime.NewDockerRuntime()
			if err != nil {
				return err
			}
			appConfig, err := config.Get()
			if err != nil {
				return fmt.Errorf("failed to get config: %w", err)
			}
			return container.Logs(cmd.Context(), rt, output.NewPlainSink(os.Stdout), appConfig.Containers, follow)
		},
	}
	cmd.Flags().BoolP("follow", "f", false, "Follow log output")
	return cmd
}
