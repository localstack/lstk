package update

import (
	"context"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/output"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An explicit update_check = "prompt" must not produce a prompt on an
// externally-managed install: pressing "Update now" would replace a binary the
// external tool owns, which is the bug this whole feature exists to prevent.
func TestNotifyUpdateExternalInstallNeverPromptsEvenWhenPromptIsExplicit(t *testing.T) {
	server := newTestGitHubServer(t, "v2.0.0")
	defer server.Close()

	var events []output.Event
	sink := output.SinkFunc(func(event output.Event) {
		events = append(events, event)
		if req, ok := event.(output.UserInputRequestEvent); ok {
			t.Error("an externally-managed install must never be prompted, even with Mode=prompt")
			req.ResponseCh() <- output.InputResponse{Cancelled: true}
		}
	})

	notifyUpdateWithVersion(context.Background(), sink, NotifyOptions{
		Mode:      config.UpdateCheckPrompt,
		CanPrompt: true,
		DetectInstall: func() InstallInfo {
			return InstallInfo{Method: InstallExternal, Manager: "mise"}
		},
	}, "1.0.0", testFetcher(server.URL))

	require.Len(t, events, 1)
	msg, ok := events[0].(output.MessageEvent)
	require.True(t, ok)
	assert.Contains(t, msg.Text, "mise")
}

// A non-interactive start emits a note too, and it must name the manager for
// the same reason: `lstk update` refuses on such an install.
func TestNotifyUpdateNonInteractiveNoteNamesTheManager(t *testing.T) {
	server := newTestGitHubServer(t, "v2.0.0")
	defer server.Close()

	var events []output.Event
	sink := output.SinkFunc(func(event output.Event) { events = append(events, event) })

	notifyUpdateWithVersion(context.Background(), sink, NotifyOptions{
		Mode:      config.UpdateCheckUnset,
		CanPrompt: false,
		DetectInstall: func() InstallInfo {
			return InstallInfo{Method: InstallExternal, Manager: "nix"}
		},
	}, "1.0.0", testFetcher(server.URL))

	require.Len(t, events, 1)
	msg, ok := events[0].(output.MessageEvent)
	require.True(t, ok)
	assert.Contains(t, msg.Text, "nix")
	assert.NotContains(t, msg.Text, "lstk update")
}

// An explicit notify on an externally-managed install must name the manager
// too — the advice is wrong there regardless of how notify was reached.
func TestNotifyUpdateExplicitNotifyNamesTheManager(t *testing.T) {
	server := newTestGitHubServer(t, "v2.0.0")
	defer server.Close()

	var events []output.Event
	sink := output.SinkFunc(func(event output.Event) { events = append(events, event) })

	notifyUpdateWithVersion(context.Background(), sink, NotifyOptions{
		Mode:      config.UpdateCheckNotify,
		CanPrompt: true,
		DetectInstall: func() InstallInfo {
			return InstallInfo{Method: InstallExternal, Manager: "asdf"}
		},
	}, "1.0.0", testFetcher(server.URL))

	require.Len(t, events, 1)
	msg := events[0].(output.MessageEvent)
	assert.Contains(t, msg.Text, "asdf")
}

// Offering "Never remind me" when there is nowhere to persist it would tell the
// user their choice was saved when it was silently dropped (the first-run case,
// where config.toml does not exist yet).
func TestNotifyUpdateOmitsNeverRemindWhenItCannotBePersisted(t *testing.T) {
	server := newTestGitHubServer(t, "v2.0.0")
	defer server.Close()

	var options []output.InputOption
	sink := output.SinkFunc(func(event output.Event) {
		if req, ok := event.(output.UserInputRequestEvent); ok {
			options = req.Options()
			req.ResponseCh() <- output.InputResponse{SelectedKey: "r"}
		}
	})

	notifyUpdateWithVersion(context.Background(), sink, NotifyOptions{
		Mode:               config.UpdateCheckPrompt,
		CanPrompt:          true,
		PersistUpdateCheck: nil,
	}, "1.0.0", testFetcher(server.URL))

	require.Len(t, options, 2)
	for _, o := range options {
		assert.NotEqual(t, "n", o.Key, "the never-ask-again option must be absent when unpersistable")
	}
}

// The prompt path's "Update now" is guarded only by applyUpdate — the
// notify-level check lets it through, because a read-only install directory is
// not an externally-managed install. Deleting applyUpdate's guard must fail a
// test, or the round-1 blocker fix is unprotected.
func TestPromptUpdateNowIsRefusedWhenTheBinaryCannotBeReplaced(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("POSIX directory permissions do not port to Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}

	server := newTestGitHubServer(t, "v2.0.0")
	defer server.Close()

	readOnly := filepath.Join(t.TempDir(), "ro")
	require.NoError(t, os.Mkdir(readOnly, 0500))
	t.Cleanup(func() { _ = os.Chmod(readOnly, 0700) })

	var events []output.Event
	sink := output.SinkFunc(func(event output.Event) {
		events = append(events, event)
		if req, ok := event.(output.UserInputRequestEvent); ok {
			req.ResponseCh() <- output.InputResponse{SelectedKey: "u"}
		}
	})

	exit := notifyUpdateWithVersion(context.Background(), sink, NotifyOptions{
		Mode:      config.UpdateCheckPrompt,
		CanPrompt: true,
		DetectInstall: func() InstallInfo {
			return InstallInfo{Method: InstallBinary, ResolvedPath: filepath.Join(readOnly, "lstk")}
		},
	}, "1.0.0", testFetcher(server.URL))

	assert.False(t, exit, "a refused update must not ask the user to re-run")

	var warned bool
	for _, e := range events {
		msg, ok := e.(output.MessageEvent)
		if !ok {
			continue
		}
		assert.NotContains(t, msg.Text, "Updated to", "no update may be reported as applied")
		if msg.Severity == output.SeverityWarning && strings.Contains(msg.Text, readOnly) {
			warned = true
		}
	}
	assert.True(t, warned, "the refusal must be surfaced, naming the directory")
}

// A refusal on the prompt path is recoverable, so it must not render as a
// persistent TUI error block (ErrorEvent sets hideHeader and stays on screen
// for the rest of the run) while the emulator start continues underneath it.
func TestPromptRefusalEmitsNoErrorEvent(t *testing.T) {
	if goruntime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("POSIX directory permissions required")
	}

	server := newTestGitHubServer(t, "v2.0.0")
	defer server.Close()

	readOnly := filepath.Join(t.TempDir(), "ro")
	require.NoError(t, os.Mkdir(readOnly, 0500))
	t.Cleanup(func() { _ = os.Chmod(readOnly, 0700) })

	var errorEvents int
	var warnings int
	sink := output.SinkFunc(func(event output.Event) {
		switch e := event.(type) {
		case output.ErrorEvent:
			errorEvents++
		case output.MessageEvent:
			if e.Severity == output.SeverityWarning {
				warnings++
			}
		case output.UserInputRequestEvent:
			e.ResponseCh() <- output.InputResponse{SelectedKey: "u"}
		}
	})

	notifyUpdateWithVersion(context.Background(), sink, NotifyOptions{
		Mode:      config.UpdateCheckPrompt,
		CanPrompt: true,
		DetectInstall: func() InstallInfo {
			return InstallInfo{Method: InstallBinary, ResolvedPath: filepath.Join(readOnly, "lstk")}
		},
	}, "1.0.0", testFetcher(server.URL))

	assert.Zero(t, errorEvents, "a recoverable refusal must not render as a failure block")
	assert.Equal(t, 1, warnings, "exactly one warning, not a duplicate")
}

// The prompt offers exactly three choices: apply, defer, or stop asking.
// "Skip this version" was removed — with a weekly release cadence it bought a
// few days of quiet, and the permanent opt-out covers the same need better.
func TestPromptOffersUpdateRemindAndNeverAskAgain(t *testing.T) {
	server := newTestGitHubServer(t, "v2.0.0")
	defer server.Close()

	var options []output.InputOption
	sink := output.SinkFunc(func(event output.Event) {
		if req, ok := event.(output.UserInputRequestEvent); ok {
			options = req.Options()
			req.ResponseCh() <- output.InputResponse{SelectedKey: "r"}
		}
	})

	notifyUpdateWithVersion(context.Background(), sink, NotifyOptions{
		Mode:               config.UpdateCheckPrompt,
		CanPrompt:          true,
		PersistUpdateCheck: func(config.UpdateCheckMode) error { return nil },
	}, "1.0.0", testFetcher(server.URL))

	require.Len(t, options, 3)
	keys := []string{options[0].Key, options[1].Key, options[2].Key}
	assert.Equal(t, []string{"u", "r", "n"}, keys)
	for _, o := range options {
		assert.NotEqual(t, "s", o.Key, "skip-this-version must be gone")
	}
}
