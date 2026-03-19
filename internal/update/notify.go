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
	GitHubToken    string
	UpdatePrompt   bool
	PersistDisable func() error
}

const checkTimeout = 500 * time.Millisecond

func CheckQuietly(ctx context.Context, githubToken string) (current, latest string, available bool) {
	return checkQuietlyWithVersion(ctx, githubToken, version.Version(), fetchLatestVersion)
}

func checkQuietlyWithVersion(ctx context.Context, githubToken string, currentVersion string, fetch versionFetcher) (current, latest string, available bool) {
	current = currentVersion
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

	if !opts.UpdatePrompt {
		output.EmitNote(sink, fmt.Sprintf("Update available: %s → %s (run lstk update)", current, latest))
		return false
	}

	return promptAndUpdate(ctx, sink, opts.GitHubToken, current, latest, opts.PersistDisable)
}

func promptAndUpdate(ctx context.Context, sink output.Sink, githubToken string, current, latest string, persistDisable func() error) (exitAfter bool) {
	output.EmitWarning(sink, fmt.Sprintf("Update available: %s → %s", current, latest))

	responseCh := make(chan output.InputResponse, 1)
	output.EmitUserInputRequest(sink, output.UserInputRequestEvent{
		Prompt: "A new version is available",
		Options: []output.InputOption{
			{Key: "u", Label: "Update"},
			{Key: "s", Label: "SKIP"},
			{Key: "n", Label: "Never ask again"},
		},
		ResponseCh: responseCh,
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
		if err := applyUpdate(ctx, sink, latest, githubToken); err != nil {
			output.EmitWarning(sink, fmt.Sprintf("Update failed: %v", err))
			return false
		}
		output.EmitSuccess(sink, fmt.Sprintf("Updated to %s — please re-run your command.", latest))
		return true
	case "n":
		if persistDisable != nil {
			if err := persistDisable(); err != nil {
				output.EmitWarning(sink, fmt.Sprintf("Failed to save preference: %v", err))
			}
		}
		return false
	default:
		return false
	}
}
