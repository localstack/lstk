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
			return runStart(cmd, rt, cfg, tel, logger)
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
		newConfigCmd(),
		newUpdateCmd(cfg, tel),
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

func startEmulator(ctx context.Context, rt runtime.Runtime, cfg *env.Env, tel *telemetry.Client, commandEventID string, logger log.Logger) error {

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
		TriggerEventID:   commandEventID,
	}

	if isInteractiveMode(cfg) {
		return ui.Run(ctx, rt, version.Version(), opts)
	}
	return container.Start(ctx, rt, output.NewPlainSink(os.Stdout), opts, false)
}

func runStart(cmd *cobra.Command, rt runtime.Runtime, cfg *env.Env, tel *telemetry.Client, logger log.Logger) error {
	startTime := time.Now()
	commandEventID := telemetry.NewEventID()

	var flags []string
	cmd.Flags().Visit(func(f *pflag.Flag) {
		flags = append(flags, "--"+f.Name)
	})

	runErr := startEmulator(cmd.Context(), rt, cfg, tel, commandEventID, logger)

	exitCode := 0
	errorMsg := ""
	if runErr != nil {
		exitCode = 1
		if !output.IsSilent(runErr) {
			errorMsg = runErr.Error()
		}
	}
	tel.Emit(cmd.Context(), "lstk_command", telemetry.ToMap(telemetry.CommandEvent{
		Environment: tel.GetEnvironment(cfg.AuthToken),
		Parameters:  telemetry.CommandParameters{Command: "start", Flags: flags},
		Result: telemetry.CommandResult{
			DurationMS: time.Since(startTime).Milliseconds(),
			ExitCode:   exitCode,
			ErrorMsg:   errorMsg,
		},
	}))

	return runErr
}

// withCommandTelemetry wraps a RunE function so that an lstk_command event is
// emitted after every invocation. Use this for commands that do not emit
// lstk_lifecycle events (i.e. everything except start/stop, which manage their
// own commandEventID for cross-event correlation).
func withCommandTelemetry(name string, tel *telemetry.Client, resolveAuthToken func() string, fn func(*cobra.Command, []string) error) func(*cobra.Command, []string) error {
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
			if !output.IsSilent(runErr) {
				errorMsg = runErr.Error()
			}
		}
		tel.Emit(cmd.Context(), "lstk_command", telemetry.ToMap(telemetry.CommandEvent{
			Environment: tel.GetEnvironment(resolveAuthToken()),
			Parameters:  telemetry.CommandParameters{Command: name, Flags: flags},
			Result: telemetry.CommandResult{
				DurationMS: time.Since(startTime).Milliseconds(),
				ExitCode:   exitCode,
				ErrorMsg:   errorMsg,
			},
		}))

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
