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
	Short:   "Stream container logs",
	Long:    "Stream logs from the LocalStack container in real-time. Press Ctrl+C to stop.",
	RunE: func(cmd *cobra.Command, args []string) error {
		rt, err := runtime.NewDockerRuntime()
		if err != nil {
			return err
		}
		return container.Logs(cmd.Context(), rt, output.NewPlainSink(os.Stdout))
	},
}

func init() {
	rootCmd.AddCommand(logsCmd)
}
