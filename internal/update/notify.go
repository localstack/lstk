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
	// Mode is resolved at the command boundary (see cmd/update_check.go) rather
	// than read from config here, keeping this package independent of Viper.
	Mode        CheckMode
	GitHubToken string

	SkippedVersion     string
	PersistSkipVersion func(version string) error

	// Install is detected once at the command boundary, so the notify wording and
	// any update applied from the prompt agree on who owns the binary.
	Install InstallInfo

	// ConfigPath is the file the "Don't ask again" choice writes to, named in its
	// confirmation.
	ConfigPath string

	// PersistCheckMode stores the policy chosen through the prompt. Nil hides the
	// "Don't ask again" option: on a first run there is no config file yet, and
	// creating one here would suppress the emulator picker, so the option is
	// withheld rather than offered as a silent no-op.
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
	// Before anything else, so "off" costs no network request, not just no output.
	if opts.Mode == CheckModeOff {
		return false
	}

	current, latest, available := checkQuietlyWithVersion(ctx, opts.GitHubToken, currentVersion, fetch)
	if !available {
		return false
	}

	// Before the mode branch, so a version skipped while prompting stays
	// suppressed after switching to notify.
	if opts.SkippedVersion != "" && normalizeVersion(opts.SkippedVersion) == normalizeVersion(latest) {
		return false
	}

	// Anything but an explicit prompt notifies: the fallthrough must be the
	// non-blocking branch so a zero-value Mode never waits on input.
	if opts.Mode != CheckModePrompt {
		sink.Emit(output.MessageEvent{Severity: output.SeverityNote, Text: notifyLine(current, latest, opts.Install.Manager)})
		return false
	}

	return promptAndUpdate(ctx, sink, opts, current, latest)
}

// Manager-owned installs point at the manager, since `lstk update` refuses.
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
		if _, err := applyUpdate(ctx, sink, opts.Install, latest, opts.GitHubToken); err != nil {
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

// persistCheckMode stores the "Don't ask again" choice. It saves notify, not off:
// the user asked not to be interrupted, not to stop hearing about releases.
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
