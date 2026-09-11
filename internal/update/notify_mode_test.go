package update

import (
	"context"
	"testing"

	"github.com/localstack/lstk/internal/output"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// failingFetcher fails the test if the version check is performed at all.
func failingFetcher(t *testing.T) versionFetcher {
	t.Helper()
	return func(ctx context.Context, token string) (string, error) {
		t.Error("no version check should be performed")
		return "", nil
	}
}

func TestNotifyUpdateOffMakesNoRequestAndNoOutput(t *testing.T) {
	var events []output.Event
	sink := output.SinkFunc(func(event output.Event) {
		events = append(events, event)
		if req, ok := event.(output.UserInputRequestEvent); ok {
			req.ResponseCh() <- output.InputResponse{Cancelled: true}
		}
	})

	exit := notifyUpdateWithVersion(context.Background(), sink, NotifyOptions{
		DetectInstall: func() InstallInfo { return InstallInfo{Method: InstallBinary} },
		CheckEnabled:  false,
		CanPrompt:     true,
	}, "1.0.0", failingFetcher(t))

	assert.False(t, exit)
	assert.Empty(t, events)
}

// A self-managed interactive install is prompted whenever the check is
// enabled. There is no user-selectable "notify" state: the non-blocking note
// is reserved for externally-managed installs and non-interactive runs.
func TestNotifyUpdateEnabledPromptsASelfManagedInstall(t *testing.T) {
	server := newTestGitHubServer(t, "v2.0.0")
	defer server.Close()

	var prompted bool
	sink := output.SinkFunc(func(event output.Event) {
		if req, ok := event.(output.UserInputRequestEvent); ok {
			prompted = true
			req.ResponseCh() <- output.InputResponse{SelectedKey: "r"}
		}
	})

	notifyUpdateWithVersion(context.Background(), sink, NotifyOptions{
		DetectInstall: func() InstallInfo { return InstallInfo{Method: InstallBinary} },
		CheckEnabled:  true,
		CanPrompt:     true,
	}, "1.0.0", testFetcher(server.URL))

	assert.True(t, prompted)
}

func TestNotifyUpdateExternalInstallDowngradesPromptToNote(t *testing.T) {
	server := newTestGitHubServer(t, "v2.0.0")
	defer server.Close()

	var events []output.Event
	sink := output.SinkFunc(func(event output.Event) {
		events = append(events, event)
		if req, ok := event.(output.UserInputRequestEvent); ok {
			t.Error("an externally-managed install must never be prompted")
			req.ResponseCh() <- output.InputResponse{Cancelled: true}
		}
	})

	exit := notifyUpdateWithVersion(context.Background(), sink, NotifyOptions{
		CheckEnabled: true,
		CanPrompt:    true,
		DetectInstall: func() InstallInfo {
			return InstallInfo{Method: InstallExternal, Manager: "mise"}
		},
	}, "1.0.0", testFetcher(server.URL))

	assert.False(t, exit)
	require.Len(t, events, 1)
	msg, ok := events[0].(output.MessageEvent)
	require.True(t, ok)
	assert.Equal(t, output.SeverityNote, msg.Severity)
}

// off must return before the version check, so detection cannot be reached.
func TestNotifyUpdateOffSkipsDetection(t *testing.T) {
	sink := output.SinkFunc(func(event output.Event) {
		if req, ok := event.(output.UserInputRequestEvent); ok {
			req.ResponseCh() <- output.InputResponse{Cancelled: true}
		}
	})

	notifyUpdateWithVersion(context.Background(), sink, NotifyOptions{
		CheckEnabled: false,
		CanPrompt:    true,
		DetectInstall: func() InstallInfo {
			t.Error("off must not consult install detection")
			return InstallInfo{}
		},
	}, "1.0.0", failingFetcher(t))
}

func TestNotifyUpdateSkipsDetectionWhenNoUpdateAvailable(t *testing.T) {
	server := newTestGitHubServer(t, "v1.0.0")
	defer server.Close()

	sink := output.SinkFunc(func(event output.Event) {
		if req, ok := event.(output.UserInputRequestEvent); ok {
			req.ResponseCh() <- output.InputResponse{Cancelled: true}
		}
	})

	notifyUpdateWithVersion(context.Background(), sink, NotifyOptions{
		CheckEnabled: true,
		CanPrompt:    true,
		DetectInstall: func() InstallInfo {
			t.Error("detection must not run before an update is known to exist")
			return InstallInfo{}
		},
	}, "v1.0.0", testFetcher(server.URL))
}

// A non-interactive call site still emits exactly one note.
func TestNotifyUpdateNonInteractiveEmitsExactlyOneNote(t *testing.T) {
	server := newTestGitHubServer(t, "v2.0.0")
	defer server.Close()

	var events []output.Event
	sink := output.SinkFunc(func(event output.Event) { events = append(events, event) })

	notifyUpdateWithVersion(context.Background(), sink, NotifyOptions{
		CheckEnabled:  true,
		CanPrompt:     false,
		DetectInstall: func() InstallInfo { return InstallInfo{Method: InstallBinary} },
	}, "1.0.0", testFetcher(server.URL))

	require.Len(t, events, 1)
}

func TestNotifyUpdateNeverAskAgainPersistsNotifyAndAppliesNoUpdate(t *testing.T) {
	server := newTestGitHubServer(t, "v2.0.0")
	defer server.Close()

	var persisted *bool
	var events []output.Event
	sink := output.SinkFunc(func(event output.Event) {
		events = append(events, event)
		if req, ok := event.(output.UserInputRequestEvent); ok {
			req.ResponseCh() <- output.InputResponse{SelectedKey: "n"}
		}
	})

	exit := notifyUpdateWithVersion(context.Background(), sink, NotifyOptions{
		DetectInstall: func() InstallInfo { return InstallInfo{Method: InstallBinary} },
		CheckEnabled:  true,
		CanPrompt:     true,
		PersistUpdateCheck: func(enabled bool) error {
			persisted = &enabled
			return nil
		},
	}, "1.0.0", testFetcher(server.URL))

	assert.False(t, exit, "choosing never-ask-again must not restart the command")
	require.NotNil(t, persisted)
	assert.False(t, *persisted, "the opt-out disables the check")
	// "applies no update" is the other half of the behavior: exit == false
	// alone would not notice an update actually being installed.
	for _, e := range events {
		if msg, ok := e.(output.MessageEvent); ok {
			assert.NotContains(t, msg.Text, "Updated to", "no update may be applied")
			assert.NotContains(t, msg.Text, "Downloading", "nothing may be downloaded")
		}
	}
}

func TestNotifyUpdateNeverAskAgainWarnsWhenPersistFails(t *testing.T) {
	server := newTestGitHubServer(t, "v2.0.0")
	defer server.Close()

	var events []output.Event
	sink := output.SinkFunc(func(event output.Event) {
		events = append(events, event)
		if req, ok := event.(output.UserInputRequestEvent); ok {
			req.ResponseCh() <- output.InputResponse{SelectedKey: "n"}
		}
	})

	exit := notifyUpdateWithVersion(context.Background(), sink, NotifyOptions{
		DetectInstall: func() InstallInfo { return InstallInfo{Method: InstallBinary} },
		CheckEnabled:  true,
		CanPrompt:     true,
		PersistUpdateCheck: func(bool) error {
			return assert.AnError
		},
	}, "1.0.0", testFetcher(server.URL))

	assert.False(t, exit)
	var warned bool
	for _, e := range events {
		if msg, ok := e.(output.MessageEvent); ok && msg.Severity == output.SeverityWarning {
			warned = true
		}
	}
	assert.True(t, warned, "a failure to persist must be surfaced as a warning")
}

// The generic note tells the user to run `lstk update`, but on an externally
// managed install that command refuses. The note must not send them at a
// command that will not work.
func TestNotifyUpdateExternalNoteNamesTheManagerNotLstkUpdate(t *testing.T) {
	server := newTestGitHubServer(t, "v2.0.0")
	defer server.Close()

	var events []output.Event
	sink := output.SinkFunc(func(event output.Event) {
		events = append(events, event)
		if req, ok := event.(output.UserInputRequestEvent); ok {
			req.ResponseCh() <- output.InputResponse{Cancelled: true}
		}
	})

	notifyUpdateWithVersion(context.Background(), sink, NotifyOptions{
		CheckEnabled: true,
		CanPrompt:    true,
		DetectInstall: func() InstallInfo {
			return InstallInfo{Method: InstallExternal, Manager: "mise"}
		},
	}, "1.0.0", testFetcher(server.URL))

	require.Len(t, events, 1)
	msg, ok := events[0].(output.MessageEvent)
	require.True(t, ok)
	assert.Contains(t, msg.Text, "mise")
	assert.NotContains(t, msg.Text, "lstk update")
}

// A note that was *not* caused by detection keeps pointing at `lstk update`,
// which is the right advice for a self-managed install.
// A self-managed install's note still points at `lstk update`, which works
// there. Reachable only when the call site cannot prompt.
func TestNotifyUpdateSelfManagedNotePointsAtLstkUpdate(t *testing.T) {
	server := newTestGitHubServer(t, "v2.0.0")
	defer server.Close()

	var events []output.Event
	sink := output.SinkFunc(func(event output.Event) { events = append(events, event) })

	notifyUpdateWithVersion(context.Background(), sink, NotifyOptions{
		DetectInstall: func() InstallInfo { return InstallInfo{Method: InstallBinary} },
		CheckEnabled:  true,
		CanPrompt:     false,
	}, "1.0.0", testFetcher(server.URL))

	require.Len(t, events, 1)
	msg, ok := events[0].(output.MessageEvent)
	require.True(t, ok)
	assert.Contains(t, msg.Text, "lstk update")
}
