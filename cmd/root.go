package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/localstack/lstk/internal/container"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "lstk",
	Short: "LocalStack CLI",
	Long:  "lstk is the command-line interface for LocalStack.",
	Run: func(cmd *cobra.Command, args []string) {
		rt, err := runtime.NewDockerRuntime()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		onProgress := func(msg string) {
			fmt.Println(msg)
		}

		if err := container.Start(cmd.Context(), rt, onProgress); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	rootCmd.AddCommand(startCmd)
}

func Execute(ctx context.Context) error {
	return rootCmd.ExecuteContext(ctx)
}
