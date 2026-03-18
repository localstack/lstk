package update

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/version"
)

// Check reports whether a newer version is available.
// Returns the latest version string and true if an update is available.
func Check(ctx context.Context, sink output.Sink, githubToken string) (string, bool, error) {
	current := version.Version()
	if current == "dev" {
		output.EmitNote(sink, "Running a development build, skipping update check")
		return "", false, nil
	}

	output.EmitSpinnerStart(sink, "Checking for updates")
	latest, err := fetchLatestVersion(ctx, githubToken)
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
func Update(ctx context.Context, sink output.Sink, checkOnly bool, githubToken string) error {
	latest, available, err := Check(ctx, sink, githubToken)
	if err != nil {
		return err
	}
	if !available || checkOnly {
		return nil
	}

	if err := applyUpdate(ctx, sink, latest, githubToken); err != nil {
		return err
	}

	output.EmitSuccess(sink, fmt.Sprintf("Updated to %s", latest))
	return nil
}

func applyUpdate(ctx context.Context, sink output.Sink, latest, githubToken string) error {
	info := DetectInstallMethod()

	var err error
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
		err = updateBinary(ctx, latest, githubToken)
		output.EmitSpinnerStop(sink)
	}
	if err != nil {
		return fmt.Errorf("update failed: %w", err)
	}

	return nil
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
			output.EmitLogLine(w.sink, w.source, line, output.LogLevelUnknown)
		}
	}
	return len(p), nil
}

// Flush emits any remaining buffered content that didn't end with a newline.
func (w *logLineWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.buf) > 0 {
		output.EmitLogLine(w.sink, w.source, string(w.buf), output.LogLevelUnknown)
		w.buf = nil
	}
}

// normalizeVersion strips a leading "v" prefix for comparison.
func normalizeVersion(v string) string {
	return strings.TrimPrefix(v, "v")
}
