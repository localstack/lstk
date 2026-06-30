package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/container"
	"github.com/localstack/lstk/internal/endpoint"
	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/extension"
	"github.com/localstack/lstk/internal/log"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/telemetry"
	"github.com/spf13/cobra"
)

// dispatchExtension is the unknown-command fallthrough: when Cobra finds no
// built-in command or alias for args[0], lstk resolves an `lstk-<name>`
// executable (bundled dir first, then PATH) and execs it, forwarding the
// remaining args verbatim and conveying runtime context via LSTK_EXT_*. Built-in
// commands never reach here because Cobra routes them to their own command, so
// they always take precedence. When no extension resolves, lstk emits its
// standard unknown-command error (to stderr, matching Cobra's own) and returns a
// silent, non-zero error. A resolved extension's invocation is recorded as a
// product-telemetry command event named "ext:<name>" so the analytics pipeline
// can track which extension ran; this is separate from the OTel span emitted
// inside extension.Invoke (see internal/extension/exec.go).
func dispatchExtension(ctx context.Context, cfg *env.Env, tel *telemetry.Client, logger log.Logger, args []string) error {
	name, extArgs := args[0], args[1:]

	resolver := extension.NewResolver(logger)
	ext, err := resolver.Resolve(name)
	if err != nil {
		if errors.Is(err, extension.ErrNotFound) {
			// Errors go to stderr, like Cobra's own unknown-command output.
			output.NewPlainSink(os.Stderr).Emit(output.ErrorEvent{
				Title:   fmt.Sprintf("unknown command %q for lstk", name),
				Actions: []output.ErrorAction{{Label: "See help:", Value: "lstk -h"}},
			})
			return output.NewSilentError(fmt.Errorf("unknown command %q for lstk", name))
		}
		return err
	}

	emulators := resolveEmulators(ctx, cfg, logger)
	configDir, err := config.ConfigDir()
	if err != nil {
		return fmt.Errorf("resolving config directory: %w", err)
	}

	runCtx := extension.Context{
		ConfigDir:      configDir,
		AuthToken:      cfg.AuthToken,
		NonInteractive: !isInteractiveMode(cfg),
		Emulators:      emulators,
	}

	logger.Info("extension: dispatching %q (bundled=%v) at %s", name, ext.Bundled, ext.Path)
	start := time.Now()
	runErr := extension.Invoke(ctx, ext, extArgs, runCtx)

	exitCode, errorMsg := 0, ""
	if runErr != nil {
		exitCode, errorMsg = 1, runErr.Error()
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
	}
	tel.EmitCommand(ctx, "ext:"+name, nil, time.Since(start).Milliseconds(), exitCode, errorMsg)

	return runErr
}

// resolveEmulators best-effort discovers every running LocalStack emulator and
// returns them for the LSTK_EXT_CONTEXT `emulators` array. lstk can run several
// emulators at once (e.g. AWS + Snowflake + Azure), so all running ones are
// reported, not just the first. When no emulator is running (or the runtime is
// unavailable) it returns nil, which Environ renders as an empty array; the
// extension is still executed.
func resolveEmulators(ctx context.Context, cfg *env.Env, logger log.Logger) []extension.Emulator {
	rt, err := runtime.NewDockerRuntime(cfg.DockerHost)
	if err != nil {
		logger.Info("extension: runtime unavailable, omitting emulator context: %v", err)
		return nil
	}
	if err := rt.IsHealthy(ctx); err != nil {
		logger.Info("extension: runtime not healthy, omitting emulator context: %v", err)
		return nil
	}

	var emulators []extension.Emulator
	for _, c := range emulatorCandidates() {
		name, err := container.ResolveRunningContainerName(ctx, rt, c)
		if err != nil || name == "" {
			continue
		}
		// Ask the runtime for the actual bound port rather than trusting config:
		// the user may have changed the config port while the container still
		// runs on the old one (mirrors `lstk status`).
		hostPort := c.Port
		if containerPort, err := c.ContainerPort(); err == nil {
			if actual, err := rt.GetBoundPort(ctx, name, containerPort); err == nil {
				hostPort = actual
			}
		}
		host, _ := endpoint.ResolveHost(ctx, hostPort, cfg.LocalStackHost)
		emulators = append(emulators, extension.Emulator{
			Type:     string(c.Type),
			Endpoint: "http://" + host,
			Port:     hostPort,
		})
	}
	return emulators
}

// emulatorCandidates returns the containers to probe for a running emulator: the
// configured containers first, then a default container for every other
// selectable emulator type, so a running emulator is found even if the config
// names a different one.
func emulatorCandidates() []config.ContainerConfig {
	var candidates []config.ContainerConfig
	seen := map[config.EmulatorType]struct{}{}

	if appCfg, err := config.Get(); err == nil {
		for _, c := range appCfg.Containers {
			candidates = append(candidates, c)
			seen[c.Type] = struct{}{}
		}
	}
	for _, t := range config.SelectableEmulatorTypes {
		if _, ok := seen[t]; ok {
			continue
		}
		candidates = append(candidates, config.ContainerConfig{Type: t, Port: config.DefaultPort})
	}
	return candidates
}

// registerExtensionHelp wires an "extensions" template function that renders the
// Extensions section of `lstk --help`. It scans the bundled dir + PATH for
// `lstk-*` executables (de-duplicated, bundled wins) and attaches descriptions
// for bundled extensions from the hand-authored descriptions file; PATH and
// custom extensions, and bundled names missing from the file, are name-only.
// Rendering never executes an extension. A scan happens on each help render so
// freshly installed extensions appear without restarting.
func registerExtensionHelp(logger log.Logger) {
	cobra.AddTemplateFunc("extensions", func(namePadding int) string {
		resolver := extension.NewResolver(logger)
		list := resolver.List()
		if len(list) == 0 {
			return ""
		}
		descriptions := extension.LoadDescriptions(resolver.BundledDir, logger)
		return formatExtensionList(list, descriptions, namePadding)
	})
}

// formatExtensionList renders the extension help lines so they align with the
// command sections above them. It mirrors Cobra's own scheme (see the usage
// template's "{{rpad .Name .NamePadding}} {{.Short}}"): each name is right-padded
// to namePadding, then a single space, then its description (bundled extensions
// only, from the descriptions file). namePadding is the root command's
// .NamePadding, so the description column matches the Commands/Tools sections; a
// name longer than namePadding widens its own row exactly as Cobra's per-row
// rpad does. Lines are sorted by name (List already sorts).
func formatExtensionList(list []extension.Extension, descriptions map[string]string, namePadding int) string {
	width := namePadding
	for _, ext := range list {
		if len(ext.Name) > width {
			width = len(ext.Name)
		}
	}

	var b strings.Builder
	for _, ext := range list {
		desc := ""
		if ext.Bundled {
			desc = descriptions[ext.Name]
		}
		if desc != "" {
			fmt.Fprintf(&b, "  %-*s %s\n", width, ext.Name, desc)
		} else {
			fmt.Fprintf(&b, "  %s\n", ext.Name)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
