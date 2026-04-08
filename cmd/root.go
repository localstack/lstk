package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/localstack/lstk/internal/api"
	"github.com/localstack/lstk/internal/auth"
	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/container"
	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/log"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/telemetry"
	"github.com/localstack/lstk/internal/ui"
	"github.com/localstack/lstk/internal/update"
	"github.com/localstack/lstk/internal/version"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func NewRootCmd(cfg *env.Env, tel *telemetry.Client, logger log.Logger) *cobra.Command {
	root := &cobra.Command{
		Use:     "lstk",
		Short:   "LocalStack CLI",
		Long:    "lstk is the command-line interface for LocalStack.",
		PreRunE: initConfig,
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := runtime.NewDockerRuntime(cfg.DockerHost)
			if err != nil {
				return err
			}
			return runStart(cmd.Context(), cmd.Flags(), rt, cfg, tel, logger)
		},
	}

	root.Version = version.Version()
	root.SilenceErrors = true
	root.SilenceUsage = true

	root.PersistentFlags().String("config", "", "Path to config file")
	root.PersistentFlags().BoolVar(&cfg.NonInteractive, "non-interactive", false, "Disable interactive mode")

	configureHelp(root)

	root.InitDefaultVersionFlag()
	root.Flags().Lookup("version").Shorthand = "v"
	root.Flags().Lookup("version").Usage = "Show version"
	root.SetVersionTemplate(versionLine() + "\n")

	root.AddCommand(
		newStartCmd(cfg, tel, logger),
		newStopCmd(cfg, tel),
		newLoginCmd(cfg, tel, logger),
		newLogoutCmd(cfg, tel, logger),
		newStatusCmd(cfg, tel),
		newLogsCmd(cfg, tel),
		newSetupCmd(cfg, tel),
		newConfigCmd(cfg, tel),
		newUpdateCmd(cfg, tel),
		newDocsCmd(),
	)

	return root
}

func Execute(ctx context.Context) error {
	cfg := env.Init()
	tel := telemetry.New(cfg.AnalyticsEndpoint, cfg.DisableEvents)
	defer tel.Close()

	logger, cleanup, err := newLogger()
	if err != nil {
		logger = log.Nop()
	}
	defer cleanup()
	logger.Info("lstk %s starting", version.Version())

	// Resolve auth token for telemetry: keyring first, then env var.
	resolvedToken := cfg.AuthToken
	if tokenStorage, err := auth.NewTokenStorage(cfg.ForceFileKeyring, logger); err == nil {
		if token, err := tokenStorage.GetAuthToken(); err == nil && token != "" {
			resolvedToken = token
		}
	}
	cfg.AuthToken = resolvedToken
	tel.SetAuthToken(resolvedToken)

	root := NewRootCmd(cfg, tel, logger)
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

func startEmulator(ctx context.Context, rt runtime.Runtime, cfg *env.Env, tel *telemetry.Client, logger log.Logger) error {

	appConfig, err := config.Get()
	if err != nil {
		return fmt.Errorf("failed to get config: %w", err)
	}

	opts := container.StartOptions{
		PlatformClient:   api.NewPlatformClient(cfg.APIEndpoint, logger),
		AuthToken:        cfg.AuthToken,
		ForceFileKeyring: cfg.ForceFileKeyring,
		WebAppURL:        cfg.WebAppURL,
		LocalStackHost:   cfg.LocalStackHost,
		Containers:       appConfig.Containers,
		Env:              appConfig.Env,
		Logger:           logger,
		Telemetry:        tel,
	}

	notifyOpts := update.NotifyOptions{
		GitHubToken:    cfg.GitHubToken,
		UpdatePrompt:   appConfig.UpdatePrompt,
		PersistDisable: config.DisableUpdatePrompt,
	}

	configPath, _ := config.FriendlyConfigPath()

	if isInteractiveMode(cfg) {
		labelCh := make(chan string, 1)
		go func() {
			label := container.ResolveEmulatorLabel(ctx, opts.PlatformClient, appConfig.Containers, cfg.AuthToken, logger)
			config.CachePlanLabel(label)
			labelCh <- label
		}()

		return ui.Run(ctx, ui.RunOptions{
			Runtime:       rt,
			Version:       version.Version(),
			StartOptions:  opts,
			NotifyOptions: notifyOpts,
			ConfigPath:    configPath,
			EmulatorLabel: config.CachedPlanLabel(),
			LabelCh:       labelCh,
		})
	}

	sink := output.NewPlainSink(os.Stdout)
	update.NotifyUpdate(ctx, sink, update.NotifyOptions{GitHubToken: cfg.GitHubToken})
	return container.Start(ctx, rt, sink, opts, false)
}

func runStart(ctx context.Context, cmdFlags *pflag.FlagSet, rt runtime.Runtime, cfg *env.Env, tel *telemetry.Client, logger log.Logger) error {
	startTime := time.Now()

	var flags []string
	cmdFlags.Visit(func(f *pflag.Flag) {
		flags = append(flags, "--"+f.Name)
	})

	runErr := startEmulator(ctx, rt, cfg, tel, logger)

	exitCode := 0
	errorMsg := ""
	if runErr != nil {
		exitCode = 1
		errorMsg = runErr.Error()
	}
	tel.EmitCommand(ctx, "start", flags, time.Since(startTime).Milliseconds(), exitCode, errorMsg)

	return runErr
}

// wraps a RunE function so that an lstk_command event is emitted after every invocation
// used for commands that do not emit lstk_lifecycle events (i.e. status, logs, config path, etc)
func commandWithTelemetry(name string, tel *telemetry.Client, fn func(*cobra.Command, []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		startTime := time.Now()
		runErr := fn(cmd, args)

		var flags []string
		cmd.Flags().Visit(func(f *pflag.Flag) {
			flags = append(flags, "--"+f.Name)
		})

		exitCode := 0
		errorMsg := ""
		if runErr != nil {
			exitCode = 1
			errorMsg = runErr.Error()
		}
		tel.EmitCommand(cmd.Context(), name, flags, time.Since(startTime).Milliseconds(), exitCode, errorMsg)

		return runErr
	}
}

func isInteractiveMode(cfg *env.Env) bool {
	return !cfg.NonInteractive && ui.IsInteractive()
}

const maxLogSize = 1 << 20 // 1 MB

func newLogger() (log.Logger, func(), error) {
	configDir, err := config.ConfigDir()
	if err != nil {
		return nil, func() {}, fmt.Errorf("resolve config directory: %w", err)
	}
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return nil, func() {}, fmt.Errorf("create config directory %s: %w", configDir, err)
	}
	path := filepath.Join(configDir, "lstk.log")
	if info, err := os.Stat(path); err == nil && info.Size() > maxLogSize {
		if err := os.Truncate(path, 0); err != nil {
			return nil, func() {}, fmt.Errorf("truncate log file %s: %w", path, err)
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return nil, func() {}, fmt.Errorf("open log file %s: %w", path, err)
	}
	return log.New(f), func() { _ = f.Close() }, nil
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
