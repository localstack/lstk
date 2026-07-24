//go:build lstk_dev

package update

import "os"

// Dev-build variants of the release URL resolvers (go build -tags lstk_dev):
// the env overrides let a local fake release server stand in for GitHub when
// manually testing the update flow. Production builds compile urls.go instead,
// so released binaries cannot be redirected via environment variables.

func releasesAPIURL() string {
	if v := os.Getenv("LSTK_DEV_API_URL"); v != "" {
		return v
	}
	return latestReleaseURL
}

func releaseDownloadBase() string {
	if v := os.Getenv("LSTK_DEV_DOWNLOAD_BASE"); v != "" {
		return v
	}
	return "https://github.com/" + githubRepo + "/releases/download"
}
