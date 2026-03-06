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
			return runStart(cmd, rt, cfg, tel)
		},
	}

	root.Version = version.Version()
	root.SetVersionTemplate(versionLine() + "\n")

	configureHelp(root)

	root.InitDefaultVersionFlag()
	root.Flags().Lookup("version").Usage = "Show version"

	root.PersistentFlags().StringP("output", "o", "text", "Output format: text or json")

	root.SilenceErrors = true
	root.SilenceUsage = true

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

	if err := root.ExecuteContext(ctx); err != nil {
		if !output.IsSilent(err) {
			if outputJSON(root) {
				sink := output.NewJSONSink(os.Stderr)
				output.EmitError(sink, output.ErrorEvent{Title: err.Error()})
			} else {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			}
		}
		return err
	}
	return nil
}

// outputJSON returns true when the user requested JSON output via --output flag.
func outputJSON(cmd *cobra.Command) bool {
	f := cmd.Root().PersistentFlags().Lookup("output")
	return f != nil && f.Value.String() == "json"
}

// useInteractiveTUI returns true when the command should use the Bubble Tea TUI.
// JSON output mode always disables the TUI.
func useInteractiveTUI(cmd *cobra.Command) bool {
	return !outputJSON(cmd) && ui.IsInteractive()
}

// newSink returns a JSONSink when --output=json, otherwise a PlainSink.
func newSink(cmd *cobra.Command) output.Sink {
	if outputJSON(cmd) {
		return output.NewJSONSink(os.Stdout)
	}
	return output.NewPlainSink(os.Stdout)
}

func runStart(cmd *cobra.Command, rt runtime.Runtime, cfg *env.Env, tel *telemetry.Client) error {
	ctx := cmd.Context()
	// TODO: replace map with a typed payload struct once event schema is finalised
	tel.Emit(ctx, "cli_cmd", map[string]any{"cmd": "lstk start", "params": []string{}})

	platformClient := api.NewPlatformClient(cfg.APIEndpoint)
	if useInteractiveTUI(cmd) {
		return ui.Run(ctx, rt, version.Version(), platformClient, cfg.AuthToken, cfg.ForceFileKeyring, cfg.WebAppURL)
	}
	return container.Start(ctx, rt, newSink(cmd), platformClient, cfg.AuthToken, cfg.ForceFileKeyring, cfg.WebAppURL, false)
}

func initConfig(_ *cobra.Command, _ []string) error {
	return config.Init()
}
