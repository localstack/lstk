package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/localstack/lstk/internal/api"
	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/container"
	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/ui"
	"github.com/localstack/lstk/internal/version"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:     "lstk",
	Short:   "LocalStack CLI",
	Long:    "lstk is the command-line interface for LocalStack.",
	PreRunE: initConfig,
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
	rootCmd.Version = version.Version()
	rootCmd.SetVersionTemplate(versionLine() + "\n")

	rootCmd.InitDefaultHelpFlag()
	rootCmd.Flags().Lookup("help").Usage = "Show help"

	rootCmd.InitDefaultVersionFlag()
	rootCmd.Flags().Lookup("version").Usage = "Show version"

	usageTemplate := rootCmd.UsageTemplate()
	usageTemplate = strings.Replace(usageTemplate, "Available Commands:", "Commands:", 1)
	usageTemplate = strings.Replace(usageTemplate, "Flags:", "Options:", 1)
	usageTemplate = strings.Replace(
		usageTemplate,
		`Usage:{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}`,
		`Usage: {{if not .HasParent}}lstk [options] [command]{{else}}{{.UseLine}}{{end}}{{if not .HasParent}}

LSTK - LocalStack command-line interface{{end}}`,
		1,
	)
	usageTemplate = strings.ReplaceAll(usageTemplate, `Use "{{.CommandPath}} [command] --help" for more information about a command.`, "")
	usageTemplate = strings.TrimRight(usageTemplate, "\n")
	rootCmd.SetUsageTemplate(usageTemplate)

	rootCmd.SetHelpTemplate(`{{if not .HasParent}}{{if or .Runnable .HasSubCommands}}{{.UsageString}}{{end}}{{else}}{{with (or .Long .Short)}}{{. | trimTrailingWhitespaces}}

{{end}}{{if or .Runnable .HasSubCommands}}{{.UsageString}}{{end}}{{end}}`)
	rootCmd.AddCommand(startCmd)
}

func Execute(ctx context.Context) error {
	return rootCmd.ExecuteContext(ctx)
}

func runStart(ctx context.Context, rt runtime.Runtime) error {
	platformClient := api.NewPlatformClient()
	if ui.IsInteractive() {
		return ui.Run(ctx, rt, version.Version(), platformClient)
	}
	return container.Start(ctx, rt, output.NewPlainSink(os.Stdout), platformClient, false)
}

func initConfig(_ *cobra.Command, _ []string) error {
	env.Init()
	return config.Init()
}
