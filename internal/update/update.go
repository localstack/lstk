// Package update implements lstk's self-update: checking GitHub for a newer
// release and applying it through whichever mechanism installed lstk (Homebrew,
// npm, or replacing the binary in place).
//
// On the binary channel an update installs the whole set a release archive
// carries (lstk, the bundled-extensions binary, lstk-extensions.toml) with a
// stage-then-commit scheme (extract.go). The guarantees to preserve:
//
//  1. A file under its real name is never truncated or half-written: content
//     is only ever written to a fresh staging file and renamed into place.
//  2. An interrupted update is repaired by re-running `lstk update`: nothing
//     commits until everything is staged, leftovers are cleaned first, and
//     lstk commits last, so a working lstk always remains. Windows caveat: a
//     crash between renaming lstk.exe aside and renaming the new one in leaves
//     no lstk.exe; rename lstk.exe.old back by hand.
//  3. Nothing is deleted: an lstk-* file absent from the archive is left alone,
//     because the updater cannot tell a dropped extension from a user's file.
//
// An archive carrying only lstk installs exactly as before bundling existed.
//
// The pre-bundling updater installs a bundling release with only its lstk
// binary. A release build that then finds neither bundle member beside itself
// (DetectMissingBundle) points the user at a reinstall, from the
// unknown-command error and from an up-to-date `lstk update`.
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

// Check reports whether a newer version is available. Returns the latest
// version string and true if an update is available. Always emits exactly one
// UpdateCheckedEvent, whose DevBuild/Available fields tell the sink which of
// the three possible outcomes (dev build skipped / already up to date / an
// update is available) occurred.
func Check(ctx context.Context, sink output.Sink, githubToken string) (string, bool, error) {
	current := version.Version()
	if current == "dev" {
		sink.Emit(output.UpdateCheckedEvent{CurrentVersion: current, DevBuild: true})
		return "", false, nil
	}

	sink.Emit(output.SpinnerStart("Checking for updates"))
	latest, err := fetchLatestVersion(ctx, githubToken)
	sink.Emit(output.SpinnerStop())
	if err != nil {
		wrapped := fmt.Errorf("failed to check for updates: %w", err)
		sink.Emit(output.ErrorEvent{Title: wrapped.Error(), Code: output.ErrNetworkError})
		return "", false, output.NewSilentError(wrapped)
	}

	available := normalizeVersion(current) != normalizeVersion(latest)
	sink.Emit(output.UpdateCheckedEvent{CurrentVersion: current, LatestVersion: latest, Available: available})
	return latest, available, nil
}

// Update checks for updates and applies the update if one is available.
func Update(ctx context.Context, sink output.Sink, checkOnly bool, githubToken string) error {
	current := version.Version()
	latest, available, err := Check(ctx, sink, githubToken)
	if err != nil {
		return err
	}
	if !available {
		warnIfBundleMissing(sink)
		return nil
	}
	if checkOnly {
		return nil
	}

	method, err := applyUpdate(ctx, sink, latest, githubToken)
	if err != nil {
		sink.Emit(output.ErrorEvent{Title: err.Error(), Code: output.ErrInternal})
		return output.NewSilentError(err)
	}

	sink.Emit(output.UpdateAppliedEvent{CurrentVersion: current, UpdatedVersion: latest, Method: method})
	return nil
}

// warnIfBundleMissing tells a current install that lacks its bundle that no
// update will bring it: the state the pre-bundling updater leaves behind.
func warnIfBundleMissing(sink output.Sink) {
	missing, ok := DetectMissingBundle()
	if !ok {
		return
	}
	sink.Emit(output.MessageEvent{
		Severity: output.SeverityWarning,
		Text:     missing.Summary() + " Reinstall lstk: " + missing.Reinstall,
	})
}

// applyUpdate detects the current install method and performs the update,
// returning its canonical name ("homebrew"/"npm"/"binary") on success.
func applyUpdate(ctx context.Context, sink output.Sink, latest, githubToken string) (string, error) {
	info := DetectInstallMethod()

	var err error
	switch info.Method {
	case InstallHomebrew:
		sink.Emit(output.MessageEvent{Severity: output.SeverityNote, Text: "Installed through Homebrew, running brew upgrade"})
		err = updateHomebrew(ctx, sink)
	case InstallNPM:
		sink.Emit(output.MessageEvent{Severity: output.SeverityNote, Text: "Installed through npm, running npm install -g"})
		err = updateNPM(ctx, sink)
	default:
		sink.Emit(output.SpinnerStart("Downloading and verifying update"))
		err = newBinaryUpdater().update(ctx, latest, githubToken)
		sink.Emit(output.SpinnerStop())
	}
	if err != nil {
		return "", fmt.Errorf("update failed: %w", err)
	}

	return info.Method.String(), nil
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
			w.sink.Emit(output.LogLineEvent{Source: w.source, Line: line, Level: output.LogLevelUnknown})
		}
	}
	return len(p), nil
}

// Flush emits any remaining buffered content that didn't end with a newline.
func (w *logLineWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.buf) > 0 {
		w.sink.Emit(output.LogLineEvent{Source: w.source, Line: string(w.buf), Level: output.LogLevelUnknown})
		w.buf = nil
	}
}

// normalizeVersion strips a leading "v" prefix for comparison.
func normalizeVersion(v string) string {
	return strings.TrimPrefix(v, "v")
}
