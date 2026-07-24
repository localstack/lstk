//go:build !lstk_dev

package update

// releasesAPIURL returns the GitHub API endpoint used to resolve the latest
// release. In production builds this is fixed; a dev build
// (go build -tags lstk_dev) allows overriding it via LSTK_DEV_API_URL — see
// urls_dev.go.
func releasesAPIURL() string {
	return latestReleaseURL
}

// releaseDownloadBase returns the base URL release assets are downloaded
// from. Fixed in production builds; overridable via LSTK_DEV_DOWNLOAD_BASE in
// dev builds — see urls_dev.go.
func releaseDownloadBase() string {
	return "https://github.com/" + githubRepo + "/releases/download"
}
