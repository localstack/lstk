package update

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"

	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/version"
)

const (
	githubRepo       = "localstack/lstk"
	latestReleaseURL = "https://api.github.com/repos/" + githubRepo + "/releases/latest"
)

type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// Check reports whether a newer version is available.
// Returns the latest version string and true if an update is available.
func Check(ctx context.Context, sink output.Sink) (string, bool, error) {
	current := version.Version()
	if current == "dev" {
		output.EmitNote(sink, "Running a development build, skipping update check")
		return "", false, nil
	}

	output.EmitSpinnerStart(sink, "Checking for updates")
	latest, err := fetchLatestVersion(ctx)
	output.EmitSpinnerStop(sink)
	if err != nil {
		return "", false, fmt.Errorf("failed to check for updates: %w", err)
	}

	if normalizeVersion(current) == normalizeVersion(latest) {
		output.EmitNote(sink, fmt.Sprintf("Already up to date (%s)", current))
		return latest, false, nil
	}

	output.EmitInfo(sink, fmt.Sprintf("Update available: %s → %s", current, latest))
	return latest, true, nil
}

// Update checks for updates and applies the update if one is available.
func Update(ctx context.Context, sink output.Sink, checkOnly bool) error {
	latest, available, err := Check(ctx, sink)
	if err != nil {
		return err
	}
	if !available || checkOnly {
		return nil
	}

	info := DetectInstallMethod()

	switch info.Method {
	case InstallHomebrew:
		output.EmitNote(sink, "Installed through Homebrew, running brew upgrade")
		err = updateHomebrew(ctx, sink)
	case InstallNPM:
		projectDir := npmProjectDir(info.ResolvedPath)
		if projectDir != "" {
			output.EmitNote(sink, fmt.Sprintf("Installed through npm (local), running npm install in %s", projectDir))
		} else {
			output.EmitNote(sink, "Installed through npm (global), running npm install -g")
		}
		err = updateNPM(ctx, sink, projectDir)
	default:
		output.EmitSpinnerStart(sink, "Downloading update")
		err = updateBinary(ctx, latest)
		output.EmitSpinnerStop(sink)
	}
	if err != nil {
		return fmt.Errorf("update failed: %w", err)
	}

	output.EmitSuccess(sink, fmt.Sprintf("Updated to %s", latest))
	return nil
}

func fetchLatestVersion(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned %s", resp.Status)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", err
	}

	return release.TagName, nil
}

func updateHomebrew(ctx context.Context, sink output.Sink) error {
	cmd := exec.CommandContext(ctx, "brew", "upgrade", "localstack/tap/lstk")
	w := newLogLineWriter(sink, "brew")
	cmd.Stdout = w
	cmd.Stderr = w
	err := cmd.Run()
	w.Flush()
	return err
}

func updateNPM(ctx context.Context, sink output.Sink, projectDir string) error {
	var cmd *exec.Cmd
	if projectDir != "" {
		cmd = exec.CommandContext(ctx, "npm", "install", "@localstack/lstk")
		cmd.Dir = projectDir
	} else {
		cmd = exec.CommandContext(ctx, "npm", "install", "-g", "@localstack/lstk")
	}
	w := newLogLineWriter(sink, "npm")
	cmd.Stdout = w
	cmd.Stderr = w
	err := cmd.Run()
	w.Flush()
	return err
}

// logLineWriter adapts an output.Sink into an io.Writer, emitting each
// complete line as a LogLineEvent. Partial writes are buffered until a
// newline arrives.
type logLineWriter struct {
	mu     sync.Mutex
	sink   output.Sink
	source string
	buf    []byte
}

func newLogLineWriter(sink output.Sink, source string) *logLineWriter {
	return &logLineWriter{sink: sink, source: source}
}

func (w *logLineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		line := string(w.buf[:i])
		w.buf = w.buf[i+1:]
		if line != "" {
			output.EmitLogLine(w.sink, w.source, line)
		}
	}
	return len(p), nil
}

// Flush emits any remaining buffered content that didn't end with a newline.
func (w *logLineWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.buf) > 0 {
		output.EmitLogLine(w.sink, w.source, string(w.buf))
		w.buf = nil
	}
}

func updateBinary(ctx context.Context, tag string) error {
	ver := normalizeVersion(tag)
	assetName := buildAssetName(ver, goruntime.GOOS, goruntime.GOARCH)

	downloadURL := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", githubRepo, tag, assetName)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: %s", resp.Status)
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot determine executable path: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return fmt.Errorf("cannot resolve executable path: %w", err)
	}

	tmpFile, err := os.CreateTemp(filepath.Dir(exe), "lstk-update-*")
	if err != nil {
		return fmt.Errorf("cannot create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("download write failed: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
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

// normalizeVersion strips a leading "v" prefix for comparison.
func normalizeVersion(v string) string {
	return strings.TrimPrefix(v, "v")
}
