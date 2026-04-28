package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/localstack/lstk/internal/api"
	"github.com/localstack/lstk/internal/auth"
	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/container"
	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/log"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/telemetry"
	"github.com/localstack/lstk/internal/tracing"
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
			return startEmulator(cmd.Context(), rt, cfg, tel, logger)
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
		newRestartCmd(cfg, tel, logger),
		newLoginCmd(cfg, tel, logger),
		newLogoutCmd(cfg, logger),
		newStatusCmd(cfg),
		newLogsCmd(cfg),
		newSetupCmd(cfg),
		newConfigCmd(cfg),
		newVolumeCmd(cfg),
		newUpdateCmd(cfg),
		newDocsCmd(),
		newAWSCmd(cfg, tel),
		newSnapshotCmd(cfg, tel),
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

	shutdownTracing := func(context.Context) error { return nil }
	if cfg.TracesEnabled {
		logger.Info("otel tracing enabled")
		shutdownTracing = tracing.Init(ctx, logger)
	}
	defer func() {
		// Use a fresh context: the parent ctx may already be cancelled (e.g. Ctrl+C)
		// by the time this defer runs, which would cause Shutdown to return immediately
		// without flushing buffered spans to the collector.
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdownTracing(shutCtx); err != nil {
			logger.Error("failed to shut down tracing: %v", err)
		}
	}()
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
	instrumentCommands(root, tel)
	if cfg.TracesEnabled {
		wrapCommandsWithTracing(root)
	}

	if err := root.ExecuteContext(ctx); err != nil {
		if !output.IsSilent(err) {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}
		return err
	}
	return nil
}

func buildStartOptions(cfg *env.Env, appConfig *config.Config, logger log.Logger, tel *telemetry.Client) container.StartOptions {
	return container.StartOptions{
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
}

func startEmulator(ctx context.Context, rt runtime.Runtime, cfg *env.Env, tel *telemetry.Client, logger log.Logger) error {

	appConfig, err := config.Get()
	if err != nil {
		return fmt.Errorf("failed to get config: %w", err)
	}

	opts := buildStartOptions(cfg, appConfig, logger, tel)

	notifyOpts := update.NotifyOptions{
		GitHubToken:        cfg.GitHubToken,
		UpdatePrompt:       true,
		SkippedVersion:     appConfig.CLI.UpdateSkippedVersion,
		PersistSkipVersion: config.SetUpdateSkippedVersion,
	}

	configPath, err := config.FriendlyConfigPath()
	if err != nil {
		logger.Info("could not resolve friendly config path: %v", err)
	}

	if isInteractiveMode(cfg) {
		labelCh := make(chan string, 1)
		go func() {
			label, ok := container.ResolveEmulatorLabel(ctx, opts.PlatformClient, appConfig.Containers, cfg.AuthToken, logger)
			if ok {
				config.CachePlanLabel(label)
			}
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

// instrumentCommands walks the Cobra command tree and wraps every RunE with telemetry emission.
func instrumentCommands(cmd *cobra.Command, tel *telemetry.Client) {
	if cmd.RunE != nil {
		original := cmd.RunE
		cmd.RunE = func(c *cobra.Command, args []string) error {
			startTime := time.Now()
			runErr := original(c, args)

			var flags []string
			c.Flags().Visit(func(f *pflag.Flag) {
				flags = append(flags, "--"+f.Name)
			})

			exitCode := 0
			errorMsg := ""
			if runErr != nil {
				exitCode = 1
				errorMsg = runErr.Error()
			}

			commandName := strings.TrimPrefix(c.CommandPath(), c.Root().Name()+" ")
			if c == c.Root() {
				commandName = "start"
			}
			tel.EmitCommand(c.Context(), commandName, flags, time.Since(startTime).Milliseconds(), exitCode, errorMsg)

			return runErr
		}
	}
	for _, child := range cmd.Commands() {
		instrumentCommands(child, tel)
	}
}

// wrapCommandsWithTracing walks the Cobra command tree and wraps every RunE with
// an OTel span. This is done once after the tree is built so individual commands
// don't need to know about tracing at all.
func wrapCommandsWithTracing(cmd *cobra.Command) {
	if cmd.RunE != nil {
		original := cmd.RunE
		spanName := strings.ReplaceAll(cmd.CommandPath(), " ", ".")
		cmd.RunE = func(c *cobra.Command, args []string) error {
			ctx, span := otel.Tracer("github.com/localstack/lstk").Start(c.Context(), spanName)
			defer span.End()
			c.SetContext(ctx)

			err := original(c, args)
			if err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
				span.SetAttributes(attribute.Int("lstk.exit_code", 1))
			} else {
				span.SetAttributes(attribute.Int("lstk.exit_code", 0))
			}
			return err
		}
	}
	for _, child := range cmd.Commands() {
		wrapCommandsWithTracing(child)
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
