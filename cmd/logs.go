package cmd

import (
	"os"

	"github.com/localstack/lstk/internal/container"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/spf13/cobra"
)

var logsCmd = &cobra.Command{
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
		return container.Logs(cmd.Context(), rt, output.NewPlainSink(os.Stdout), follow)
	},
}

func init() {
	rootCmd.AddCommand(logsCmd)
	logsCmd.Flags().BoolP("follow", "f", false, "Follow log output")
}
