package cmd

import (
	"fmt"
	"os"
	"strconv"

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
		Long:    "Show logs from the emulator. Use --follow to stream in real-time and --tail to limit output to the last N lines.",
		PreRunE: initConfigDeferCreate(nil),
		RunE: func(cmd *cobra.Command, args []string) error {
			follow, err := cmd.Flags().GetBool("follow")
			if err != nil {
				return err
			}
			verbose, err := cmd.Flags().GetBool("verbose")
			if err != nil {
				return err
			}
			tail, err := cmd.Flags().GetString("tail")
			if err != nil {
				return err
			}
			if err := validateTail(tail); err != nil {
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
				return ui.RunLogs(cmd.Context(), rt, appConfig.Containers, follow, tail, verbose)
			}
			return container.Logs(cmd.Context(), rt, output.NewPlainSink(os.Stdout), appConfig.Containers, follow, tail, verbose)
		},
	}
	cmd.Flags().BoolP("follow", "f", false, "Follow log output")
	cmd.Flags().BoolP("verbose", "v", false, "Show all log output without filtering")
	cmd.Flags().StringP("tail", "n", "all", "Number of lines to show from the end of the logs")
	return cmd
}

func validateTail(tail string) error {
	if tail == "all" {
		return nil
	}
	if n, err := strconv.Atoi(tail); err != nil || n < 0 {
		return fmt.Errorf("invalid --tail value %q: expected a non-negative integer or \"all\"", tail)
	}
	return nil
}
