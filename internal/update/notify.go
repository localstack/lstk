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
	GitHubToken          string
	UpdatePrompt         bool
	SkippedVersion       string
	PersistSkipVersion   func(version string) error
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
	current, latest, available := checkQuietlyWithVersion(ctx, opts.GitHubToken, currentVersion, fetch)
	if !available {
		return false
	}

	if opts.SkippedVersion != "" && normalizeVersion(opts.SkippedVersion) == normalizeVersion(latest) {
		return false
	}

	if !opts.UpdatePrompt {
		sink.Emit(output.MessageEvent{Severity: output.SeverityNote, Text: fmt.Sprintf("Update available: %s → %s (run lstk update)", current, latest)})
		return false
	}

	return promptAndUpdate(ctx, sink, opts, current, latest)
}

func promptAndUpdate(ctx context.Context, sink output.Sink, opts NotifyOptions, current, latest string) (exitAfter bool) {
	releaseNotesURL := fmt.Sprintf("https://github.com/%s/releases/latest", githubRepo)

	sink.Emit(output.MessageEvent{Severity: output.SeverityNote, Text: fmt.Sprintf("New lstk version available! %s → %s", current, latest)})
	sink.Emit(output.MessageEvent{Severity: output.SeveritySecondary, Text: fmt.Sprintf("> Release notes: %s", releaseNotesURL)})

	responseCh := make(chan output.InputResponse, 1)
	sink.Emit(output.UserInputRequestEvent{
		Prompt:     "Update lstk to latest version?",
		Options:    []output.InputOption{{Key: "u", Label: "Update now [U]"}, {Key: "r", Label: "Remind me next time [R]"}, {Key: "s", Label: "Skip this version [S]"}},
		ResponseCh: responseCh,
		Vertical:   true,
	})

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
		if err := applyUpdate(ctx, sink, latest, opts.GitHubToken); err != nil {
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
	}

	return false
}
