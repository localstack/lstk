package update

import (
	"context"
	"fmt"
	"time"

	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/version"
)

type versionFetcher func(ctx context.Context, token string) (string, error)

type NotifyOptions struct {
	// Mode is the resolved update-check policy. It is resolved at the command
	// boundary (see cmd/update_check.go) rather than read from config here, so
	// this package stays independent of Viper.
	Mode        CheckMode
	GitHubToken string

	SkippedVersion     string
	PersistSkipVersion func(version string) error

	// Manager is the external package manager that owns this install, or empty
	// when lstk owns its own binary. It only changes the wording of the notify
	// line — whether the prompt is shown at all is decided by Mode.
	Manager ExternalManager

	// ConfigPath is the friendly path of the file the "Don't ask again" choice
	// writes to, named in that choice's confirmation.
	ConfigPath string

	// PersistCheckMode stores the policy chosen through the prompt. A nil value
	// hides the "Don't ask again" option entirely: on a genuine first run there
	// is no config file yet, and creating one here would suppress the emulator
	// picker (see config.EnsureCreated's callers), so the option is withheld
	// rather than offered as a silent no-op.
	PersistCheckMode func(mode CheckMode) error
}

const checkTimeout = 2 * time.Second

func checkQuietlyWithVersion(ctx context.Context, githubToken string, currentVersion string, fetch versionFetcher) (current, latest string, available bool) {
	current = currentVersion
	// Skip update check for dev builds
	if current == "dev" {
		return current, "", false
	}

	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	latestVer, err := fetch(ctx, githubToken)
	if err != nil {
		return current, "", false
	}

	if normalizeVersion(current) == normalizeVersion(latestVer) {
		return current, latestVer, false
	}

	return current, latestVer, true
}

func NotifyUpdate(ctx context.Context, sink output.Sink, opts NotifyOptions) (exitAfter bool) {
	return notifyUpdateWithVersion(ctx, sink, opts, version.Version(), fetchLatestVersion)
}

func notifyUpdateWithVersion(ctx context.Context, sink output.Sink, opts NotifyOptions, currentVersion string, fetch versionFetcher) (exitAfter bool) {
	// Checked before anything else so "off" costs no network request at all,
	// not merely no output.
	if opts.Mode == CheckModeOff {
		return false
	}

	current, latest, available := checkQuietlyWithVersion(ctx, opts.GitHubToken, currentVersion, fetch)
	if !available {
		return false
	}

	// Gated before the mode branch so a version skipped while prompting stays
	// suppressed after the user switches to notify.
	if opts.SkippedVersion != "" && normalizeVersion(opts.SkippedVersion) == normalizeVersion(latest) {
		return false
	}

	// Anything other than an explicit prompt policy notifies. The fallthrough is
	// deliberately the non-blocking branch: a zero-value Mode must never leave
	// the CLI waiting on input.
	if opts.Mode != CheckModePrompt {
		sink.Emit(output.MessageEvent{Severity: output.SeverityNote, Text: notifyLine(current, latest, opts.Manager)})
		return false
	}

	return promptAndUpdate(ctx, sink, opts, current, latest)
}

// notifyLine is the one-line, non-blocking update notice. Installs owned by
// another package manager point at that manager instead of `lstk update`, which
// refuses on them.
func notifyLine(current, latest string, manager ExternalManager) string {
	if manager != "" {
		return fmt.Sprintf("Update available: %s → %s (installed with %s — %s)", current, latest, manager.DisplayName(), manager.UpgradeAdvice())
	}
	return fmt.Sprintf("Update available: %s → %s (run lstk update)", current, latest)
}

func promptAndUpdate(ctx context.Context, sink output.Sink, opts NotifyOptions, current, latest string) (exitAfter bool) {
	releaseNotesURL := fmt.Sprintf("https://github.com/%s/releases/latest", githubRepo)

	sink.Emit(output.MessageEvent{Severity: output.SeverityNote, Text: fmt.Sprintf("New lstk version available! %s → %s", current, latest)})
	sink.Emit(output.MessageEvent{Severity: output.SeveritySecondary, Text: fmt.Sprintf("> Release notes: %s", releaseNotesURL)})

	options := []output.InputOption{
		{Key: "u", Label: "Update now"},
		{Key: "r", Label: "Remind me next time"},
		{Key: "s", Label: "Skip this version"},
	}
	if opts.PersistCheckMode != nil {
		options = append(options, output.InputOption{Key: "n", Label: "Don't ask again"})
	}

	responseCh := make(chan output.InputResponse, 1)
	sink.Emit(output.ActionChoice("Update lstk to latest version?", options, responseCh))

	var resp output.InputResponse
	select {
	case resp = <-responseCh:
	case <-ctx.Done():
		return false
	}

	if resp.Cancelled {
		return false
	}

	switch resp.SelectedKey {
	case "u":
		if _, err := applyUpdate(ctx, sink, latest, opts.GitHubToken); err != nil {
			sink.Emit(output.MessageEvent{Severity: output.SeverityWarning, Text: fmt.Sprintf("Update failed: %v", err)})
			return false
		}
		sink.Emit(output.MessageEvent{Severity: output.SeveritySuccess, Text: fmt.Sprintf("Updated to %s — please re-run your command.", latest)})
		return true
	case "r":
		return false
	case "s":
		if opts.PersistSkipVersion != nil {
			if err := opts.PersistSkipVersion(latest); err != nil {
				sink.Emit(output.MessageEvent{Severity: output.SeverityWarning, Text: fmt.Sprintf("Failed to persist skipped version: %v", err)})
			}
		}
		sink.Emit(output.MessageEvent{Severity: output.SeverityNote, Text: "Skipping version " + latest})
		return false
	case "n":
		persistCheckMode(sink, opts)
		return false
	}

	return false
}

// persistCheckMode stores the "Don't ask again" choice. It saves notify rather
// than off on purpose: the user asked not to be interrupted, not to stop hearing
// about releases, so the one-line note stays and the confirmation says how to
// reach the other two modes.
func persistCheckMode(sink output.Sink, opts NotifyOptions) {
	if err := opts.PersistCheckMode(CheckModeNotify); err != nil {
		sink.Emit(output.MessageEvent{Severity: output.SeverityWarning, Text: fmt.Sprintf("Failed to save update check preference: %v", err)})
		return
	}

	target := opts.ConfigPath
	if target == "" {
		target = "your lstk config file"
	}
	sink.Emit(output.MessageEvent{
		Severity: output.SeverityNote,
		Text:     fmt.Sprintf("Won't ask again — saved update_check = %q to %s", CheckModeNotify, target),
	})
	sink.Emit(output.MessageEvent{
		Severity: output.SeveritySecondary,
		Text:     `> One-line update notes still appear; set update_check to "prompt" to be asked again, or "off" to disable the check`,
	})
}
