package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/localstack/lstk/internal/api"
	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/container"
	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/telemetry"
	"github.com/localstack/lstk/internal/ui"
	"github.com/localstack/lstk/internal/version"
	"github.com/spf13/cobra"
)

func NewRootCmd(cfg *env.Env, tel *telemetry.Client) *cobra.Command {
	root := &cobra.Command{
		Use:     "lstk",
		Short:   "LocalStack CLI",
		Long:    "lstk is the command-line interface for LocalStack.",
		PreRunE: initConfig,
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := runtime.NewDockerRuntime()
			if err != nil {
				return err
			}
			return runStart(cmd.Context(), rt, cfg, tel)
		},
	}

	root.Version = version.Version()
	root.SetVersionTemplate(versionLine() + "\n")
	root.SilenceErrors = true
	root.SilenceUsage = true

	root.PersistentFlags().String("config", "", "Path to config file")

	configureHelp(root)

	root.InitDefaultVersionFlag()
	root.Flags().Lookup("version").Usage = "Show version"

	root.AddCommand(
		newStartCmd(cfg, tel),
		newStopCmd(),
		newLoginCmd(cfg),
		newLogoutCmd(cfg),
		newLogsCmd(),
		newConfigCmd(),
		newVersionCmd(),
	)

	return root
}

func Execute(ctx context.Context) error {
	cfg := env.Init()
	tel := telemetry.New(cfg.AnalyticsEndpoint, cfg.DisableEvents)
	defer tel.Close()

	root := NewRootCmd(cfg, tel)
	root.SilenceErrors = true
	root.SilenceUsage = true

	if err := root.ExecuteContext(ctx); err != nil {
		if !output.IsSilent(err) {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}
		return err
	}
	return nil
}

func runStart(ctx context.Context, rt runtime.Runtime, cfg *env.Env, tel *telemetry.Client) error {
	// TODO: replace map with a typed payload struct once event schema is finalised
	tel.Emit(ctx, "cli_cmd", map[string]any{"cmd": "lstk start", "params": []string{}})

	platformClient := api.NewPlatformClient(cfg.APIEndpoint)
	if ui.IsInteractive() {
		return ui.Run(ctx, rt, version.Version(), platformClient, cfg.AuthToken, cfg.ForceFileKeyring, cfg.WebAppURL, cfg.LocalStackHost)
	}
	return container.Start(ctx, rt, output.NewPlainSink(os.Stdout), platformClient, cfg.AuthToken, cfg.ForceFileKeyring, cfg.WebAppURL, false, cfg.LocalStackHost)
}

func initConfig(cmd *cobra.Command, _ []string) error {
	path, err := cmd.Flags().GetString("config")
	if err != nil {
		return err
	}
	if path != "" {
		return config.InitFromPath(path)
	}
	return config.Init()
}
