package cmd

import (
	"fmt"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/update"
)

// updateCheckContext is what resolveUpdateCheckMode needs to pick a policy,
// gathered at the boundary so the resolution stays a pure function.
type updateCheckContext struct {
	EnvValue          string // LSTK_UPDATE_CHECK, empty when unset
	ConfigValue       string // [cli] update_check, empty when unset
	ExternallyManaged bool   // another package manager owns the binary
	Interactive       bool   // a prompt could actually be answered
}

// resolveUpdateCheckMode resolves the policy in precedence order:
// LSTK_UPDATE_CHECK, [cli] update_check, then the install-implied default
// (notify when a package manager owns the binary, prompt otherwise).
//
// An unparsable value is reported and skipped, never fatal: the setting governs
// a best-effort background check, so a typo must not stop `lstk start` — and
// there is no `lstk config set` to fix it with. Falling through source by source
// keeps the documented precedence true even when one source is garbage.
//
// Off a terminal, prompt is downgraded to notify: only the TUI answers a
// UserInputRequestEvent, so a plain sink would block until context cancellation.
func resolveUpdateCheckMode(sink output.Sink, checkCtx updateCheckContext) update.CheckMode {
	mode := resolveConfiguredCheckMode(sink, checkCtx)
	if mode == update.CheckModePrompt && !checkCtx.Interactive {
		return update.CheckModeNotify
	}
	return mode
}

func resolveConfiguredCheckMode(sink output.Sink, checkCtx updateCheckContext) update.CheckMode {
	sources := []struct {
		label string
		value string
	}{
		{"LSTK_UPDATE_CHECK", checkCtx.EnvValue},
		{"update_check in [cli]", checkCtx.ConfigValue},
	}

	for _, source := range sources {
		if source.value == "" {
			continue
		}
		mode, err := update.ParseCheckMode(source.value)
		if err != nil {
			sink.Emit(output.MessageEvent{
				Severity: output.SeverityWarning,
				Text:     fmt.Sprintf("Ignoring %s: %v", source.label, err),
			})
			continue
		}
		return mode
	}

	if checkCtx.ExternallyManaged {
		return update.CheckModeNotify
	}
	return update.CheckModePrompt
}

// buildNotifyOptions resolves the one policy both start paths use. Building it
// once is what keeps them in sync: the non-interactive path used to construct
// its own NotifyOptions and so ignored the skipped version (DEVX-1029).
func buildNotifyOptions(sink output.Sink, cfg *env.Env, appConfig *config.Config, configPath string, firstRun, interactive bool) update.NotifyOptions {
	info := update.DetectInstallMethod()

	opts := update.NotifyOptions{
		Mode: resolveUpdateCheckMode(sink, updateCheckContext{
			EnvValue:          cfg.UpdateCheck,
			ConfigValue:       appConfig.CLI.UpdateCheck,
			ExternallyManaged: info.ExternallyManaged(),
			Interactive:       interactive,
		}),
		GitHubToken:        cfg.GitHubToken,
		SkippedVersion:     appConfig.CLI.UpdateSkippedVersion,
		PersistSkipVersion: config.SetUpdateSkippedVersion,
		Install:            info,
		ConfigPath:         configPath,
	}

	// No config file to write to on a first run, and creating one here would
	// suppress the emulator picker — so withhold the option (see NotifyOptions).
	if !firstRun {
		opts.PersistCheckMode = func(mode update.CheckMode) error {
			return config.SetUpdateCheck(string(mode))
		}
	}

	return opts
}
