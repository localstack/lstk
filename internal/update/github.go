package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"
)

const githubRepo = "localstack/lstk"

const latestReleaseURL = "https://api.github.com/repos/" + githubRepo + "/releases/latest"

type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func githubRequest(ctx context.Context, url, token string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return http.DefaultClient.Do(req)
}

func fetchLatestRelease(ctx context.Context, token string) (*githubRelease, error) {
	resp, err := githubRequest(ctx, latestReleaseURL, token)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %s", resp.Status)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}

	return &release, nil
}

func fetchLatestVersion(ctx context.Context, token string) (string, error) {
	release, err := fetchLatestRelease(ctx, token)
	if err != nil {
		return "", err
	}
	return release.TagName, nil
}

// binaryUpdater performs the direct-binary update path: download the release
// archive, verify its SHA-256 against the release's checksums.txt, and replace
// the running executable. Fields exist so tests can point downloads at a local
// server and resolve a fake executable.
type binaryUpdater struct {
	downloadBase string
	resolveExe   func() (string, error)
}

func newBinaryUpdater() *binaryUpdater {
	return &binaryUpdater{
		downloadBase: "https://github.com/" + githubRepo + "/releases/download",
		resolveExe: func() (string, error) {
			exe, err := os.Executable()
			if err != nil {
				return "", fmt.Errorf("cannot determine executable path: %w", err)
			}
			exe, err = filepath.EvalSymlinks(exe)
			if err != nil {
				return "", fmt.Errorf("cannot resolve executable path: %w", err)
			}
			return exe, nil
		},
	}
}

// maxChecksumsSize bounds the checksums.txt download; the real manifest is a
// few hundred bytes.
const maxChecksumsSize = 1 << 20

func (u *binaryUpdater) fetchChecksums(ctx context.Context, tag, token string) (map[string]string, error) {
	url := fmt.Sprintf("%s/%s/checksums.txt", u.downloadBase, tag)
	resp, err := githubRequest(ctx, url, token)
	if err != nil {
		return nil, fmt.Errorf("failed to download checksum manifest: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("release %s has no checksums.txt asset; refusing to install an unverifiable binary", tag)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("checksum manifest download failed: %s", resp.Status)
	}

	return parseChecksums(io.LimitReader(resp.Body, maxChecksumsSize))
}

func (u *binaryUpdater) update(ctx context.Context, tag, token string) error {
	ver := normalizeVersion(tag)
	assetName := buildAssetName(ver, goruntime.GOOS, goruntime.GOARCH)

	sums, err := u.fetchChecksums(ctx, tag, token)
	if err != nil {
		return err
	}
	expectedSum, ok := sums[assetName]
	if !ok {
		return fmt.Errorf("checksums.txt for release %s has no entry for %s; refusing to install an unverifiable binary", tag, assetName)
	}

	downloadURL := fmt.Sprintf("%s/%s/%s", u.downloadBase, tag, assetName)

	resp, err := githubRequest(ctx, downloadURL, token)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: %s", resp.Status)
	}

	exe, err := u.resolveExe()
	if err != nil {
		return err
	}

	tmpFile, err := os.CreateTemp(filepath.Dir(exe), "lstk-update-*")
	if err != nil {
		return fmt.Errorf("cannot create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmpFile, hasher), resp.Body); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("download write failed: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	actualSum := hex.EncodeToString(hasher.Sum(nil))
	if actualSum != expectedSum {
		return fmt.Errorf("checksum mismatch for %s: expected %s, got %s — the downloaded archive may be corrupted or tampered with; update aborted", assetName, expectedSum, actualSum)
	}

	if goruntime.GOOS == "windows" {
		return extractAndReplace(tmpPath, exe, "zip")
	}
	return extractAndReplace(tmpPath, exe, "tar.gz")
}

func buildAssetName(ver, goos, goarch string) string {
	ext := "tar.gz"
	if goos == "windows" {
		ext = "zip"
	}
	return fmt.Sprintf("lstk_%s_%s_%s.%s", ver, goos, goarch, ext)
}
