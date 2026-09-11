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
//  3. Nothing is deleted: only the members the archive carries are written, so
//     an archive without the bundle (a rollback) replaces lstk alone.
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
	"errors"
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

// Update checks for updates and applies one if available.
//
// When it would apply, it first refuses installs it must not replace (see
// blockSelfUpdate) — ahead of the version check and download, so an install
// lstk cannot write to fails immediately rather than after fetching and
// verifying an archive it can never install. --check is exempt: it writes
// nothing, and its answer is useful however lstk was installed.
func Update(ctx context.Context, sink output.Sink, checkOnly bool, githubToken string, force bool) error {
	info := DetectInstallMethod()
	// Probes twice per `lstk update` (here and in applyUpdate), deliberately:
	// applyUpdate stays the choke point every replacing path goes through,
	// while this check keeps the refusal ahead of the download. Two cheap
	// probes on a command that would otherwise fetch megabytes.
	if !checkOnly && !force {
		if blocker := blockSelfUpdate(info); blocker != nil {
			return emitSelfUpdateBlocked(sink, blocker)
		}
	}

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

	method, blocker, err := applyUpdate(ctx, sink, latest, githubToken, force, info)
	if blocker != nil {
		return emitSelfUpdateBlocked(sink, blocker)
	}
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

// emitSelfUpdateBlocked renders a refusal to replace lstk's own binary and
// returns the silent error the caller should propagate.
func emitSelfUpdateBlocked(sink output.Sink, blocker *selfUpdateBlocker) error {
	sink.Emit(output.ErrorEvent{
		Title:   blocker.title(),
		Summary: blocker.summary(),
		Actions: []output.ErrorAction{blocker.action()},
		Code:    output.ErrUpdateExternallyManaged,
	})
	return output.NewSilentError(errors.New(blocker.title()))
}

// applyUpdate performs the update for an already-detected install method,
// returning its canonical name ("homebrew"/"npm"/"binary") on success.
//
// Its blockSelfUpdate check is the choke point: every path that replaces the
// binary comes through here, including the start-path prompt's "Update now" —
// guarding only the `lstk update` entry point left that prompt able to clobber
// an externally-managed install. It returns a non-nil blocker rather than
// updating, so each caller picks the severity: `lstk update` fails with an
// ErrorEvent, the prompt warns and carries on.
func applyUpdate(ctx context.Context, sink output.Sink, latest, githubToken string, force bool, info InstallInfo) (string, *selfUpdateBlocker, error) {
	if !force {
		if blocker := blockSelfUpdate(info); blocker != nil {
			return "", blocker, nil
		}
	}

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
		return "", nil, fmt.Errorf("update failed: %w", err)
	}

	return appliedMethodName(info.Method), nil, nil
}

// appliedMethodName maps an install method to the name reported in the
// UpdateAppliedEvent (and so in the --json envelope's "method" field), whose
// documented values are homebrew/npm/binary. InstallExternal reports "binary"
// because the only way it reaches here is `lstk update --force`, which performs
// exactly the binary replacement — the field says how the update happened, not
// how lstk was originally installed.
func appliedMethodName(m InstallMethod) string {
	switch m {
	case InstallHomebrew:
		return "homebrew"
	case InstallNPM:
		return "npm"
	default:
		return "binary"
	}
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
