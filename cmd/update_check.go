package cmd

import (
	"fmt"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/update"
)

// updateCheckContext is everything resolveUpdateCheckMode needs to pick a
// policy, gathered at the command boundary so the resolution itself stays a pure
// function over named inputs.
type updateCheckContext struct {
	// EnvValue is LSTK_UPDATE_CHECK, empty when unset.
	EnvValue string
	// ConfigValue is [cli] update_check, empty when unset.
	ConfigValue string
	// ExternallyManaged reports that another package manager owns the binary.
	ExternallyManaged bool
	// Interactive reports whether a prompt could actually be answered.
	Interactive bool
}

// resolveUpdateCheckMode resolves the automatic update-check policy in
// precedence order: LSTK_UPDATE_CHECK, then [cli] update_check in config.toml,
// then the default implied by the install (notify when another package manager
// owns the binary, prompt otherwise).
//
// An unparsable value is reported through sink and skipped rather than failing
// the command. The setting governs only a best-effort background check, so a
// typo must never stop `lstk start` — and it cannot be fixed with lstk itself,
// since there is no `lstk config set`. Falling through source by source (rather
// than jumping straight to the default) keeps the documented precedence true
// even when one source is garbage.
//
// A prompt is downgraded to notify off an interactive terminal: only the TUI
// answers a UserInputRequestEvent, so prompting against a plain sink would block
// the CLI until the context is cancelled with nothing on screen to act on.
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

// buildNotifyOptions resolves the single update-notification policy used by both
// the interactive (TUI) and non-interactive start paths. Building it once is
// what keeps them in sync: the non-interactive path used to construct its own
// NotifyOptions and so ignored the skipped version entirely (DEVX-1029).
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
		Manager:            info.Manager,
		ConfigPath:         configPath,
	}

	// On a genuine first run there is no config file to write to yet, and
	// creating one here would suppress the emulator picker, so the prompt's
	// "Don't ask again" option is withheld rather than silently doing nothing.
	if !firstRun {
		opts.PersistCheckMode = func(mode update.CheckMode) error {
			return config.SetUpdateCheck(string(mode))
		}
	}

	return opts
}
