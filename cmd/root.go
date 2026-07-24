package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
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
	"github.com/localstack/lstk/internal/validate"
	"github.com/localstack/lstk/internal/version"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// canonicalCommandAnnotation, when set on a cobra.Command, overrides the
// command path reported to telemetry and tracing. Used so root-level aliases
// emit the same name as their canonical subcommand.
const canonicalCommandAnnotation = "lstk.canonical"

// jsonSupportedAnnotation, when set on a cobra.Command, opts that command into
// --json output. Commands without this annotation reject --json instead of
// silently rendering plain text; see requireJSONSupport.
const jsonSupportedAnnotation = "lstk.jsonSupported"

// Command group IDs used to separate the proxy "tool" commands (aws, terraform,
// cdk, sam, az) from the rest of lstk's commands in the help output.
const (
	groupCommands = "commands"
	groupTools    = "tools"
)

func NewRootCmd(cfg *env.Env, tel *telemetry.Client, logger log.Logger) *cobra.Command {
	var firstRun bool
	root := &cobra.Command{
		Use:     "lstk",
		Short:   "LocalStack CLI",
		Long:    "lstk is the command-line interface for LocalStack.",
		PreRunE: initConfigDeferCreate(&firstRun),
		// ArbitraryArgs stops Cobra from rejecting an unknown first arg with its
		// own "unknown command" error before RunE runs, so an unknown `lstk
		// <name>` falls through to extension dispatch. Built-in commands are still
		// matched by Cobra's command resolution first, so they always win.
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// A non-empty arg here means the first positional was not a built-in
			// command (Cobra would have routed those to their own command), so it
			// is an extension name; everything after it is forwarded verbatim.
			if len(args) > 0 {
				return dispatchExtension(cmd.Context(), cfg, tel, logger, args)
			}
			rt, err := runtime.NewDockerRuntime(cfg.DockerHost)
			if err != nil {
				return err
			}
			persist, err := cmd.Flags().GetBool("persist")
			if err != nil {
				return err
			}
			snapshotFlag, noSnapshot, err := snapshotFlags(cmd)
			if err != nil {
				return err
			}
			// A non-empty first positional was already routed to extension dispatch
			// above, so the emulator is selected only via the --type flag here.
			emulatorType, err := resolveEmulatorTypeFlag(cmd)
			if err != nil {
				return err
			}
			if err := applyTimeoutFlag(cmd, cfg); err != nil {
				return err
			}
			return startEmulator(cmd.Context(), rt, cfg, tel, logger, persist, firstRun, snapshotFlag, noSnapshot, emulatorType)
		},
	}

	root.Version = version.Version()
	root.SilenceErrors = true
	root.SilenceUsage = true

	// Flag-parsing failures (e.g. an unknown flag, or a malformed value for an
	// ordinary bool flag) happen inside Cobra's own ParseFlags, before any
	// RunE — including requireJSONSupport/wrapCommandsWithJSONEnvelope below — ever runs.
	// This is the one hook Cobra offers into that path, so it is the only place
	// a usage error can be rendered as the JSON envelope (error.code:
	// USAGE_ERROR) rather than falling through to the plain-text fallback in
	// Execute(). cfg.JSON reliably reflects "was --json already recognized by
	// the time parsing failed", since pflag parses flags left-to-right and
	// binds each directly to its variable as it succeeds.
	root.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		if !cfg.JSON {
			return err
		}
		commandName := commandDisplayName(c)
		envelope := output.Envelope{
			SchemaVersion: output.EnvelopeSchemaVersion,
			Command:       commandName,
			Status:        output.StatusError,
			Warnings:      []output.Warning{},
			Error: &output.EnvelopeError{
				Code:      output.ErrUsageError,
				Category:  output.ErrUsageError.Category(),
				Message:   err.Error(),
				Retryable: output.ErrUsageError.Retryable(),
			},
		}
		if writeErr := writeEnvelope(os.Stdout, envelope); writeErr != nil {
			return writeErr
		}
		return output.NewSilentError(err)
	})

	root.PersistentFlags().String("config", "", "Path to config file")
	root.PersistentFlags().BoolVar(&cfg.NonInteractive, "non-interactive", false, "Disable interactive mode")
	root.PersistentFlags().BoolVar(&cfg.JSON, "json", false, "Output in JSON format (only supported by some commands)")
	root.Flags().Bool("persist", false, "Persist emulator state across restarts")
	addEmulatorTypeFlag(root)
	addSnapshotStartFlags(root)
	addTimeoutFlag(root)

	// Parse lstk's global flags only when they precede the command name: with
	// interspersing disabled, Cobra consumes leading flags and hands everything
	// from the first positional (the command/extension name) onward to the
	// dispatch path verbatim. This gives Git-style "globals only before the
	// command" and lets an extension own its entire flag space — a flag after the
	// name (even one named like an lstk global) is forwarded untouched. Only the
	// root's own flag set is affected; built-in subcommands keep their own
	// (interspersing) flag parsing.
	root.Flags().SetInterspersed(false)

	configureHelp(root)
	registerExtensionHelp(logger)

	root.InitDefaultVersionFlag()
	root.Flags().Lookup("version").Shorthand = "v"
	root.Flags().Lookup("version").Usage = "Show version"
	root.SetVersionTemplate(versionLine() + "\n")

	root.AddGroup(
		&cobra.Group{ID: groupCommands, Title: "Commands:"},
		&cobra.Group{ID: groupTools, Title: "Tools:"},
	)

	commands := []*cobra.Command{
		newStartCmd(cfg, tel, logger),
		newStopCmd(cfg, tel),
		newRestartCmd(cfg, tel, logger),
		newLoginCmd(cfg, tel, logger),
		newLogoutCmd(cfg, logger),
		newStatusCmd(cfg),
		newLogsCmd(cfg),
		newSetupCmd(cfg),
		newConfigCmd(),
		newVolumeCmd(cfg),
		newUpdateCmd(cfg),
		newDocsCmd(),
		newSnapshotCmd(cfg, tel, logger),
		newResetCmd(cfg),
		newSaveCmd(cfg),
		newLoadCmd(cfg, tel, logger),
	}
	for _, c := range commands {
		c.GroupID = groupCommands
	}

	// Proxy commands that forward to a wrapped tool (AWS/Azure CLI, Terraform,
	// CDK, SAM) configured to target LocalStack.
	tools := []*cobra.Command{
		newAWSCmd(cfg),
		newTerraformCmd(cfg, logger),
		newCDKCmd(cfg, logger),
		newSamCmd(cfg, logger),
		newAzCmd(cfg),
	}
	for _, c := range tools {
		c.GroupID = groupTools
	}

	root.AddCommand(commands...)
	root.AddCommand(tools...)

	root.SetHelpCommandGroupID(groupCommands)
	root.SetCompletionCommandGroupID(groupCommands)

	// Cobra's autogenerated completion command is itself a subcommand-grouping
	// parent (bash/zsh/fish/powershell) with no RunE, so an unknown shell (e.g.
	// `lstk completion bogus`) hits the same exit-0 path requireSubcommand fixes.
	// It is created lazily during Execute, so force it now to wire it up too.
	root.InitDefaultCompletionCmd()
	if completionCmd, _, err := root.Find([]string{"completion"}); err == nil && completionCmd.Name() == "completion" {
		requireSubcommand(completionCmd)
		selfContainBashCompletion(completionCmd)
	}

	return root
}

// requireSubcommand configures a parent command that only groups subcommands so
// an unknown or missing subcommand exits non-zero instead of Cobra's default of
// printing help and exiting 0. Cobra only validates args (and so rejects unknown
// subcommands via cobra.NoArgs) when the command is runnable, hence the RunE that
// prints help for a bare invocation.
func requireSubcommand(cmd *cobra.Command) {
	cmd.Args = cobra.NoArgs
	cmd.RunE = func(c *cobra.Command, _ []string) error {
		return c.Help()
	}
}

func Execute(ctx context.Context) error {
	if len(os.Args) > 1 && os.Args[1] == telemetry.FlushCommandName {
		return runFlushTelemetry(ctx, os.Args[2:])
	}

	cfg := env.Init()

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

	tel := telemetry.New(cfg.AnalyticsEndpoint, cfg.DisableEvents)
	defer tel.Close()

	logger.Info("lstk %s starting", version.Version())

	// Resolve auth token for telemetry: keyring first, then env var.
	resolvedToken := cfg.AuthToken
	if tokenStorage, err := auth.NewTokenStorage(cfg.ForceFileKeyring, logger); err == nil {
		if token, err := tokenStorage.GetAuthToken(); err == nil && token != "" {
			resolvedToken = token
		}
	}
	// Trim surrounding whitespace: env-injected tokens (e.g. CI secrets) commonly
	// carry a trailing newline. Then reject clearly malformed tokens before they
	// reach the platform API, telemetry, or the container environment.
	resolvedToken = strings.TrimSpace(resolvedToken)
	if err := validate.AuthToken(resolvedToken); err != nil {
		err = fmt.Errorf("invalid auth token: %w", err)
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return err
	}
	cfg.AuthToken = resolvedToken
	tel.SetAuthToken(resolvedToken)

	root := NewRootCmd(cfg, tel, logger)
	root.SilenceErrors = true
	root.SilenceUsage = true
	requireJSONSupport(root, cfg)
	instrumentCommands(root, tel)
	if cfg.TracesEnabled {
		wrapCommandsWithTracing(root)
	}
	wrapCommandsWithJSONEnvelope(root, cfg, os.Stdout)
	wrapPreRunEForJSON(root, cfg, os.Stdout)

	if err := root.ExecuteContext(ctx); err != nil {
		if !output.IsSilent(err) {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}
		return err
	}
	return nil
}

func buildStartOptions(cfg *env.Env, appConfig *config.Config, logger log.Logger, tel *telemetry.Client, persist bool) container.StartOptions {
	return container.StartOptions{
		PlatformClient:   api.NewPlatformClient(cfg.APIEndpoint, logger),
		AuthToken:        cfg.AuthToken,
		ForceFileKeyring: cfg.ForceFileKeyring,
		WebAppURL:        cfg.WebAppURL,
		LocalStackHost:   cfg.LocalStackHost,
		Containers:       appConfig.Containers,
		Env:              appConfig.Env,
		Persist:          persist,
		StartupTimeout:   cfg.StartupTimeout,
		Logger:           logger,
		Telemetry:        tel,
	}
}

func startEmulator(ctx context.Context, rt runtime.Runtime, cfg *env.Env, tel *telemetry.Client, logger log.Logger, persist bool, firstRun bool, snapshotFlag string, noSnapshot bool, emulatorType config.EmulatorType) error {
	appConfig, err := config.Get()
	if err != nil {
		return fmt.Errorf("failed to get config: %w", err)
	}

	configPath, err := config.FriendlyConfigPath()
	if err != nil {
		logger.Info("could not resolve friendly config path: %v", err)
	}

	// Apply the --type flag before resolving snapshot and start options so
	// everything downstream reflects the selected emulator. Messages go to a plain
	// sink even in interactive mode because the config mutation has to happen before
	// the TUI starts (the auto-load loader and start options are built from it).
	if emulatorType != "" {
		newContainers, applyErr := container.ApplyEmulatorType(ctx, rt, output.NewPlainSink(os.Stdout), emulatorType, appConfig.Containers, firstRun, configPath)
		if applyErr != nil {
			return applyErr
		}
		appConfig.Containers = newContainers
		// The config now exists and records the selection, so it is no longer a
		// first run: skip the interactive picker and the default-emulator notice.
		firstRun = false
	}

	ref, err := resolveStartSnapshotRef(appConfig, snapshotFlag, noSnapshot)
	if err != nil {
		return err
	}
	// Parse the REF eagerly so an invalid snapshot fails before the emulator starts.
	autoLoad, err := newSnapshotAutoLoader(cfg, rt, appConfig, ref)
	if err != nil {
		return err
	}

	opts := buildStartOptions(cfg, appConfig, logger, tel, persist)

	notifyOpts := update.NotifyOptions{
		GitHubToken:        cfg.GitHubToken,
		UpdatePrompt:       true,
		SkippedVersion:     appConfig.CLI.UpdateSkippedVersion,
		PersistSkipVersion: config.SetUpdateSkippedVersion,
	}

	if isInteractiveMode(cfg) {
		return ui.Run(ctx, ui.RunOptions{
			Runtime:                rt,
			Version:                version.Version(),
			StartOptions:           opts,
			NotifyOptions:          notifyOpts,
			ConfigPath:             configPath,
			EmulatorLabel:          config.CachedPlanLabel(),
			NeedsEmulatorSelection: firstRun,
			PostStart:              autoLoad,
		})
	}

	sink := output.NewPlainSink(os.Stdout)
	if firstRun && len(appConfig.Containers) > 0 {
		emName := appConfig.Containers[0].Type.ShortName()
		sink.Emit(output.MessageEvent{
			Severity: output.SeverityNote,
			Text:     fmt.Sprintf("Configured with default emulator %s.", emName),
		})
	}
	update.NotifyUpdate(ctx, sink, update.NotifyOptions{GitHubToken: cfg.GitHubToken})
	resolvedVersion, err := container.Start(ctx, rt, sink, opts, false)
	if err != nil {
		return err
	}
	// Auto-load the configured snapshot only when the emulator was freshly started
	// this run (resolvedVersion is empty when it was already running). This mirrors
	// v1's AUTO_LOAD_POD: state is loaded as the emulator comes up, not on every invocation.
	if autoLoad != nil && resolvedVersion != "" {
		if err := autoLoad(ctx, sink); err != nil {
			return err
		}
	}
	if firstRun {
		return config.EnsureCreated()
	}
	return nil
}

// addEmulatorTypeFlag registers the --type/-t flag on a start-capable command.
func addEmulatorTypeFlag(cmd *cobra.Command) {
	cmd.Flags().StringP("type", "t", "", "Emulator type to start (aws, snowflake, azure)")
}

// resolveEmulatorTypeFlag resolves the requested emulator type from the --type
// flag. It returns "" when the flag is unset.
func resolveEmulatorTypeFlag(cmd *cobra.Command) (config.EmulatorType, error) {
	flagVal, err := cmd.Flags().GetString("type")
	if err != nil {
		return "", err
	}
	if flagVal == "" {
		return "", nil
	}
	return config.ParseEmulatorType(flagVal)
}

// addTimeoutFlag registers the --timeout flag on a start-capable command. It is
// a per-run override of LSTK_STARTUP_TIMEOUT / the startup_timeout config; 0
// (the default) leaves the env/config value in place, which in turn falls back
// to the per-mode default in resolveStartupTimeout. restart and the snapshot
// auto-start path deliberately do not expose this flag.
func addTimeoutFlag(cmd *cobra.Command) {
	cmd.Flags().Duration("timeout", 0, "Maximum time to wait for the emulator to become ready (overrides LSTK_STARTUP_TIMEOUT; 0 uses the default)")
}

// applyTimeoutFlag lets --timeout override the env/config-derived
// cfg.StartupTimeout, but only when the flag was explicitly set, so an unset
// flag preserves the LSTK_STARTUP_TIMEOUT value.
func applyTimeoutFlag(cmd *cobra.Command, cfg *env.Env) error {
	if !cmd.Flags().Changed("timeout") {
		return nil
	}
	timeout, err := cmd.Flags().GetDuration("timeout")
	if err != nil {
		return err
	}
	cfg.StartupTimeout = timeout
	return nil
}

// walkCommandsWithRunE walks the Cobra command tree rooted at cmd, calling wrap
// on every command that has a RunE so callers can layer cross-cutting behavior
// (telemetry, JSON gating, tracing) onto it.
func walkCommandsWithRunE(cmd *cobra.Command, wrap func(*cobra.Command)) {
	if cmd.RunE != nil {
		wrap(cmd)
	}
	for _, child := range cmd.Commands() {
		walkCommandsWithRunE(child, wrap)
	}
}

// walkCommandsWithPreRunE walks the Cobra command tree rooted at cmd, calling
// wrap on every command that has a PreRunE, mirroring walkCommandsWithRunE
// above for callers that need to layer behavior onto the pre-RunE step
// instead (e.g. wrapPreRunEForJSON).
func walkCommandsWithPreRunE(cmd *cobra.Command, wrap func(*cobra.Command)) {
	if cmd.PreRunE != nil {
		wrap(cmd)
	}
	for _, child := range cmd.Commands() {
		walkCommandsWithPreRunE(child, wrap)
	}
}

// isExtensionDispatch reports whether the RunE invocation is the root command
// invoked with a positional arg, i.e. extension dispatch. Extension dispatch
// owns its own output format and command-event reporting, so telemetry and
// JSON-support gating both skip it.
func isExtensionDispatch(c *cobra.Command, args []string) bool {
	return c == c.Root() && len(args) > 0
}

// commandDisplayName returns the human-readable name for a command used in
// telemetry and error messages: the canonicalCommandAnnotation override if
// present, "start" for the bare root command, otherwise the command path with
// the root command name trimmed off.
func commandDisplayName(c *cobra.Command) string {
	if canonical, ok := c.Annotations[canonicalCommandAnnotation]; ok {
		return canonical
	}
	if c == c.Root() {
		return "start"
	}
	return strings.TrimPrefix(c.CommandPath(), c.Root().Name()+" ")
}

// ExitCode maps a command error to the exit code the lstk process terminates
// with: a proxied tool's *exec.ExitError carries that tool's exact code, an
// output.ExitCodeError carries the --json exit-code convention (3
// CONFIRMATION_REQUIRED, 4 AUTH_REQUIRED), anything else collapses to 1.
// errors.As unwraps through the SilentError wrapper to reach either type.
// main.go and instrumentCommands both use this, so the telemetry exit_code
// always matches the real process exit code.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	var codeErr *output.ExitCodeError
	if errors.As(err, &codeErr) {
		return codeErr.Code
	}
	return 1
}

// instrumentCommands walks the Cobra command tree and wraps every RunE with telemetry emission.
func instrumentCommands(cmd *cobra.Command, tel *telemetry.Client) {
	walkCommandsWithRunE(cmd, func(c *cobra.Command) {
		original := c.RunE
		c.RunE = func(c *cobra.Command, args []string) error {
			startTime := time.Now()
			runErr := original(c, args)

			// Extension dispatch records its own command event in dispatchExtension,
			// which knows the resolved extension name; skip the generic emit here so
			// the invocation is not mislabeled as "start".
			if isExtensionDispatch(c, args) {
				return runErr
			}

			var flags []string
			c.Flags().Visit(func(f *pflag.Flag) {
				flags = append(flags, "--"+f.Name)
			})

			// Proxy commands disable flag parsing, so their wrapped tool's
			// subcommand is invisible in the command path; record its leading
			// tokens so failures are attributable to a service/operation.
			subcommand := ""
			if c.DisableFlagParsing {
				subcommand = proxySubcommand(args)
			}

			exitCode := ExitCode(runErr)
			errorMsg := ""
			if runErr != nil {
				errorMsg = runErr.Error()
			}

			tel.EmitCommand(c.Context(), commandDisplayName(c), subcommand, flags, time.Since(startTime).Milliseconds(), exitCode, errorMsg)

			return runErr
		}
	})
}

// requireJSONSupport walks the Cobra command tree and wraps every RunE so that,
// when cfg.JSON is set, a command lacking the jsonSupportedAnnotation is
// rejected instead of silently rendering plain-text output. The rejection
// itself renders as the standard JSON envelope (error.code: NOT_JSON_CAPABLE)
// since the invocation explicitly asked for JSON — there is no
// command-specific EnvelopeSink to pull from here, so the envelope is built
// directly.
func requireJSONSupport(cmd *cobra.Command, cfg *env.Env) {
	walkCommandsWithRunE(cmd, func(c *cobra.Command) {
		original := c.RunE
		c.RunE = func(c *cobra.Command, args []string) error {
			if isExtensionDispatch(c, args) {
				return original(c, args)
			}

			if cfg.JSON {
				if _, ok := c.Annotations[jsonSupportedAnnotation]; !ok {
					commandName := commandDisplayName(c)
					message := fmt.Sprintf("%q is not able to provide output in JSON format", commandName)
					envelope := output.Envelope{
						SchemaVersion: output.EnvelopeSchemaVersion,
						Command:       commandName,
						Status:        output.StatusError,
						Warnings:      []output.Warning{},
						Error: &output.EnvelopeError{
							Code:      output.ErrNotJSONCapable,
							Category:  output.ErrNotJSONCapable.Category(),
							Message:   message,
							Retryable: output.ErrNotJSONCapable.Retryable(),
						},
					}
					if err := writeEnvelope(os.Stdout, envelope); err != nil {
						return err
					}
					return output.NewSilentError(fmt.Errorf("%s: not able to provide output in JSON format", commandName))
				}
			}

			return original(c, args)
		}
	})
}

// wrapCommandsWithTracing walks the Cobra command tree and wraps every RunE with
// an OTel span. This is done once after the tree is built so individual commands
// don't need to know about tracing at all.
func wrapCommandsWithTracing(cmd *cobra.Command) {
	walkCommandsWithRunE(cmd, func(c *cobra.Command) {
		original := c.RunE
		spanName := strings.ReplaceAll(c.CommandPath(), " ", ".")
		if canonical, ok := c.Annotations[canonicalCommandAnnotation]; ok {
			spanName = strings.ReplaceAll(c.Root().Name()+" "+canonical, " ", ".")
		}
		c.RunE = func(c *cobra.Command, args []string) error {
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
	})
}

func isInteractiveMode(cfg *env.Env) bool {
	return !cfg.NonInteractive && !cfg.JSON && ui.IsInteractive()
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

func initConfigDeferCreate(firstRun *bool) func(*cobra.Command, []string) error {
	return initConfigWith(firstRun, config.Load)
}

func initConfigWith(firstRun *bool, load func() (bool, error)) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		path, err := cmd.Flags().GetString("config")
		if err != nil {
			return err
		}
		if path != "" {
			return config.InitFromPath(path)
		}
		isFirstRun, err := load()
		if firstRun != nil {
			*firstRun = isFirstRun
		}
		return err
	}
}
