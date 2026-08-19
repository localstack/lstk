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
	// Refused before Check so an install lstk must not touch costs no network
	// request either. --check is read-only and stays allowed: knowing a new
	// version exists is useful even when another tool installs it.
	if !checkOnly {
		if err := refuseExternalUpdate(sink, DetectInstallMethod()); err != nil {
			return err
		}
	}

	current := version.Version()
	latest, available, err := Check(ctx, sink, githubToken)
	if err != nil {
		return err
	}
	if !available || checkOnly {
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

// refuseExternalUpdate reports that lstk cannot update itself because another
// package manager owns the binary, returning the silent error the command
// boundary propagates. It returns nil for every install lstk does manage.
func refuseExternalUpdate(sink output.Sink, info InstallInfo) error {
	if !info.ExternallyManaged() {
		return nil
	}

	manager := info.Manager.DisplayName()
	err := fmt.Errorf("lstk was installed with %s, so it cannot update itself", manager)

	summary := fmt.Sprintf("%s owns this binary (%s); replacing it in place would leave %s out of sync.", manager, info.ResolvedPath, manager)
	var actions []output.ErrorAction
	if cmd := info.Manager.UpgradeCommand(); cmd != "" {
		actions = append(actions, output.ErrorAction{Label: fmt.Sprintf("Update it with %s:", manager), Value: cmd})
	} else {
		// No single correct command for this manager, so the advice goes in the
		// summary rather than masquerading as something runnable.
		summary += fmt.Sprintf(" Update it with %s instead.", manager)
	}
	actions = append(actions, output.ErrorAction{Label: "Or just check for a new version:", Value: "lstk update --check"})

	sink.Emit(output.ErrorEvent{
		Title:   err.Error(),
		Summary: summary,
		Actions: actions,
		Code:    output.ErrUpdateExternallyManaged,
	})
	return output.NewSilentError(err)
}

// applyUpdate detects the current install method and performs the update,
// returning its canonical name ("homebrew"/"npm"/"binary") on success.
func applyUpdate(ctx context.Context, sink output.Sink, latest, githubToken string) (string, error) {
	info := DetectInstallMethod()

	var err error
	switch info.Method {
	case InstallExternal:
		// Defense in depth: Update refuses before reaching here, but the
		// interactive prompt's "Update now" is also routed through applyUpdate,
		// and a user who forces update_check = "prompt" on a managed install
		// must still never have their binary replaced.
		return "", fmt.Errorf("lstk was installed with %s; %s to update it", info.Manager.DisplayName(), info.Manager.UpgradeAdvice())
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
