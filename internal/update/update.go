// Package update implements lstk's self-update: checking GitHub for a newer
// release and applying it through whichever mechanism installed lstk (Homebrew,
// npm, or replacing the binary in place).
//
// # What a binary-channel update installs
//
// An update installs the whole version-matched set a release archive carries —
// the lstk binary, the multi-call "bundled-extensions" binary that provides
// every bundled extension, and the descriptions file — not just lstk. Homebrew
// and npm get this for free by replacing the whole package; the binary channel
// implements it in extract.go as stage-then-commit: every member is copied next
// to its destination under a ".lstk-new" name, and only once all copies succeed
// is each renamed over its final name, lstk last.
//
// # The guarantee
//
// This is not atomicity across files, which POSIX cannot deliver. It is three
// specific promises, and the reason the code is shaped the way it is. Preserve
// them through any refactor:
//
//  1. A file visible under its real name is never truncated or half-written.
//     New content is only ever written to a staging name; a final name only
//     ever changes by rename, which is atomic within a directory.
//  2. An interrupted update is repaired by re-running `lstk update`. Nothing
//     is committed until every member is staged, staging files left by an
//     interrupted run are cleaned up before the next one stages, and lstk
//     itself commits last — so an interruption leaves the user either on their
//     previous complete version or on a fully committed new one, and never in
//     a state a re-run cannot resolve.
//  3. The updater never deletes an "lstk-*" file that the new archive does not
//     contain. It cannot distinguish a bundled extension a release dropped
//     from one the user put there, and the descriptions file is not an
//     ownership manifest (a bundled binary is permitted to have no
//     description). The binary channel is therefore additive-only: a dropped
//     extension keeps working and shows name-only in help, because the
//     replaced descriptions file no longer describes it. Homebrew and npm
//     remove such files naturally via whole-package replacement. See design
//     Decision 4 of the add-bundled-extension-distribution change for the full
//     reasoning.
//
// A release archive carrying only lstk — every pre-bundling release, and any
// rollback to one — is a valid set of size one and installs exactly as it did
// before bundling existed. When an archive does carry extensions they are not
// optional: a member that fails to stage or commit fails the whole update and
// names the member, rather than reporting success with a partial set.
//
// An install can also be current and still incomplete, which no version
// comparison can detect; missingBundledMembers explains when that happens and
// how `lstk update` repairs it.
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

// Check reports whether `lstk update` has work to do. Returns the latest
// version string and true when an update should be applied. Always emits
// exactly one UpdateCheckedEvent, whose DevBuild/RepairBundled/Available fields
// tell the sink which of the four possible outcomes (dev build skipped /
// already up to date / an update is available / the installed set is
// incomplete and is being repaired) occurred.
func Check(ctx context.Context, sink output.Sink, githubToken string) (string, bool, error) {
	return checkWithVersion(ctx, sink, githubToken, version.Version(), missingBundledMembers)
}

// checkWithVersion is Check with the running version and the set-completeness
// probe as parameters, mirroring checkQuietlyWithVersion, so both can be driven
// from a test.
func checkWithVersion(ctx context.Context, sink output.Sink, githubToken, current string, missingMembers func() []string) (string, bool, error) {
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

	// An install can be current and still incomplete — see
	// missingBundledMembers for how that happens and why the version
	// comparison alone cannot detect it. The probe only runs when the versions
	// match, so the ordinary up-to-date path costs exactly what it did before.
	repair := false
	if !available && len(missingMembers()) > 0 {
		available, repair = true, true
	}

	sink.Emit(output.UpdateCheckedEvent{
		CurrentVersion: current,
		LatestVersion:  latest,
		Available:      available,
		RepairBundled:  repair,
	})
	return latest, available, nil
}

// Update checks for updates and applies the update if one is available.
func Update(ctx context.Context, sink output.Sink, checkOnly bool, githubToken string) error {
	current := version.Version()
	latest, available, err := Check(ctx, sink, githubToken)
	if err != nil {
		return err
	}
	if !available || checkOnly {
		return nil
	}

	// Available with an unchanged version can only mean a bundled-set repair
	// (UpdateCheckedEvent.RepairBundled documents the equivalence). The checked
	// event states only the finding, because it also renders under --check;
	// the reinstall is narrated here, where it actually happens.
	if normalizeVersion(current) == normalizeVersion(latest) {
		sink.Emit(output.MessageEvent{
			Severity: output.SeverityNote,
			Text:     fmt.Sprintf("Reinstalling %s to restore bundled extensions", current),
		})
	}

	method, err := applyUpdate(ctx, sink, latest, githubToken)
	if err != nil {
		sink.Emit(output.ErrorEvent{Title: err.Error(), Code: output.ErrInternal})
		return output.NewSilentError(err)
	}

	sink.Emit(output.UpdateAppliedEvent{CurrentVersion: current, UpdatedVersion: latest, Method: method})
	return nil
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
