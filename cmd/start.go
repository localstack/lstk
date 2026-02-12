package cmd

import (
	"fmt"
	"os"

	"github.com/localstack/lstk/internal/api"
	"github.com/localstack/lstk/internal/container"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start LocalStack",
	Long:  "Start the LocalStack emulator.",
	Run: func(cmd *cobra.Command, args []string) {
		rt, err := runtime.NewDockerRuntime()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		platformClient := api.NewPlatformClient()

		onProgress := func(msg string) {
			fmt.Println(msg)
		}

		if err := container.Start(cmd.Context(), rt, platformClient, onProgress); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}
