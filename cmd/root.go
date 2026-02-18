package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/localstack/lstk/internal/api"
	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/container"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/spf13/cobra"
)

var version = "dev"
var commit = "none"
var buildDate = "unknown"

var rootCmd = &cobra.Command{
	Use:   "lstk",
	Short: "LocalStack CLI",
	Long:  "lstk is the command-line interface for LocalStack.",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Version should be side-effect free and must not create/read user config.
		if cmd.Name() == "version" {
			return nil
		}
		return config.Init()
	},
	Run: func(cmd *cobra.Command, args []string) {
		rt, err := runtime.NewDockerRuntime()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		if err := runStart(cmd.Context(), rt); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	rootCmd.Version = version
	rootCmd.SetVersionTemplate(versionLine() + "\n")
	rootCmd.AddCommand(startCmd)
}

func Execute(ctx context.Context) error {
	return rootCmd.ExecuteContext(ctx)
}

func runStart(ctx context.Context, rt runtime.Runtime) error {
	return container.Start(ctx, rt, output.NewPlainSink(os.Stdout), api.NewPlatformClient())
}
