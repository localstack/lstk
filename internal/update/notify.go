package update

import (
	"context"
	"fmt"
	"time"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/version"
)

type versionFetcher func(ctx context.Context, token string) (string, error)

type NotifyOptions struct {
	GitHubToken string
	// CanPrompt reports whether this call site is able to present a blocking
	// prompt at all (an interactive TTY). It is independent of Mode, which is
	// the user's preference: a non-interactive start can only ever emit a note,
	// however Mode is set.
	CanPrompt          bool
	Mode               config.UpdateCheckMode
	PersistUpdateCheck func(mode config.UpdateCheckMode) error
	// DetectInstall resolves how lstk itself was installed. Injected rather
	// than called directly so tests do not depend on where the test binary
	// happens to live — which is also what makes the apply-time guard on the
	// prompt path testable. Defaults to DetectInstallMethod when nil.
	DetectInstall func() InstallInfo
}

// installInfo resolves the install once per notification.
func (o NotifyOptions) installInfo() InstallInfo {
	if o.DetectInstall == nil {
		return DetectInstallMethod()
	}
	return o.DetectInstall()
}

const checkTimeout = 2 * time.Second

func CheckQuietly(ctx context.Context, githubToken string) (current, latest string, available bool) {
	return checkQuietlyWithVersion(ctx, githubToken, version.Version(), fetchLatestVersion)
}

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
	if opts.Mode == config.UpdateCheckOff {
		return false
	}

	current, latest, available := checkQuietlyWithVersion(ctx, opts.GitHubToken, currentVersion, fetch)
	if !available {
		return false
	}

	// Detection runs exactly once, and only now that an update is known to
	// exist — which is what keeps it off every `lstk start`. It runs regardless
	// of how the mode was set, because its answer feeds the note's wording as
	// well as the prompt/note decision, and a note that says "run lstk update"
	// on an install where that command refuses is wrong however the mode was
	// reached.
	info := opts.installInfo()
	external := info.Method == InstallExternal

	mode := opts.Mode
	if mode == config.UpdateCheckUnset {
		mode = config.UpdateCheckPrompt
	}

	// An externally-managed install is never prompted, even when the user asked
	// for prompt explicitly: "Update now" would replace a binary the external
	// tool owns, and applyUpdate refuses it anyway. Offering an action that
	// cannot be carried out is worse than not offering it.
	if !opts.CanPrompt || external || mode == config.UpdateCheckNotify {
		sink.Emit(updateNote(current, latest, info.Manager))
		return false
	}

	return promptAndUpdate(ctx, sink, opts, current, latest, info)
}

// updateNote is the non-blocking "a newer version exists" line. When manager is
// set, it names that tool instead of pointing at `lstk update` — which refuses
// on an externally-managed install, so advising it there would send the user at
// a command that cannot work.
func updateNote(current, latest, manager string) output.MessageEvent {
	text := fmt.Sprintf("Update available: %s → %s (run lstk update)", current, latest)
	if manager != "" {
		text = fmt.Sprintf("Update available: %s → %s (installed via %s — update it there)", current, latest, manager)
	}
	return output.MessageEvent{Severity: output.SeverityNote, Text: text}
}

func promptAndUpdate(ctx context.Context, sink output.Sink, opts NotifyOptions, current, latest string, info InstallInfo) (exitAfter bool) {
	releaseNotesURL := fmt.Sprintf("https://github.com/%s/releases/latest", githubRepo)

	sink.Emit(output.MessageEvent{Severity: output.SeverityNote, Text: fmt.Sprintf("New lstk version available! %s → %s", current, latest)})
	sink.Emit(output.MessageEvent{Severity: output.SeveritySecondary, Text: fmt.Sprintf("> Release notes: %s", releaseNotesURL)})

	options := []output.InputOption{
		{Key: "u", Label: "Update now"},
		{Key: "r", Label: "Remind me next time"},
	}
	// Offered only when there is somewhere to write it. On a first run
	// config.toml does not exist yet, and persisting would be silently dropped
	// — telling the user their choice was saved when it was not.
	if opts.PersistUpdateCheck != nil {
		options = append(options, output.InputOption{Key: "n", Label: "Never ask again"})
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
		// A refusal here is recoverable and the start continues, so it is
		// surfaced as a single warning. Rendering it as an ErrorEvent (what the
		// `lstk update` entry point does) would leave a persistent failure
		// block on screen while the emulator comes up underneath it.
		_, blocker, err := applyUpdate(ctx, sink, latest, opts.GitHubToken, false, info)
		if blocker != nil {
			sink.Emit(output.MessageEvent{
				Severity: output.SeverityWarning,
				Text:     fmt.Sprintf("%s (%s). %s", blocker.title(), blocker.summary(), blocker.action().Label),
			})
			return false
		}
		if err != nil {
			sink.Emit(output.MessageEvent{Severity: output.SeverityWarning, Text: fmt.Sprintf("Update failed: %v", err)})
			return false
		}
		sink.Emit(output.MessageEvent{Severity: output.SeveritySuccess, Text: fmt.Sprintf("Updated to %s — please re-run your command.", latest)})
		return true
	case "r":
		return false
	case "n":
		// Persists notify rather than off: the user asked to stop being
		// interrupted, which is not the same as asking never to hear about a
		// release again. Silencing entirely stays a deliberate config edit.
		if opts.PersistUpdateCheck == nil {
			// Unreachable while the option is only offered when the hook is set
			// (see above), but a future edit that always appends it must warn
			// rather than panic mid-start.
			sink.Emit(output.MessageEvent{Severity: output.SeverityWarning, Text: "Cannot save update preference: no config file"})
			return false
		}
		if err := opts.PersistUpdateCheck(config.UpdateCheckNotify); err != nil {
			sink.Emit(output.MessageEvent{Severity: output.SeverityWarning, Text: fmt.Sprintf("Failed to save update preference: %v", err)})
			return false
		}
		sink.Emit(output.MessageEvent{Severity: output.SeverityNote, Text: "Won't ask again — new versions will show as a note. Run lstk update to update."})
		return false
	}

	return false
}
