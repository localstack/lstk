package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/localstack/lstk/internal/output"
	"github.com/stretchr/testify/assert"
)

func newTestGitHubServer(t *testing.T, tagName string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := githubRelease{TagName: tagName}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatal(err)
		}
	}))
}

func testFetcher(serverURL string) versionFetcher {
	return func(ctx context.Context, token string) (string, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, serverURL, nil)
		if err != nil {
			return "", err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", err
		}
		defer func() { _ = resp.Body.Close() }()
		var release githubRelease
		if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
			return "", err
		}
		return release.TagName, nil
	}
}

// failingFetcher fails the test if it is called at all, so a caller can assert
// that no version request was made rather than merely that nothing was printed.
func failingFetcher(t *testing.T) versionFetcher {
	t.Helper()
	return func(ctx context.Context, token string) (string, error) {
		t.Error("version check performed a request when it should not have")
		return "", nil
	}
}

func TestCheckQuietlyDevBuild(t *testing.T) {
	current, latest, available := checkQuietlyWithVersion(context.Background(), "", "dev", failingFetcher(t))
	assert.Equal(t, "dev", current)
	assert.Empty(t, latest)
	assert.False(t, available)
}

func TestCheckQuietlyNetworkError(t *testing.T) {
	fetch := func(ctx context.Context, token string) (string, error) {
		return "", fmt.Errorf("connection refused")
	}

	current, latest, available := checkQuietlyWithVersion(context.Background(), "", "1.0.0", fetch)
	assert.Equal(t, "1.0.0", current)
	assert.Empty(t, latest)
	assert.False(t, available)
}

func TestCheckQuietlyUpdateAvailable(t *testing.T) {
	server := newTestGitHubServer(t, "v2.0.0")
	defer server.Close()

	current, latest, available := checkQuietlyWithVersion(context.Background(), "", "1.0.0", testFetcher(server.URL))
	assert.Equal(t, "1.0.0", current)
	assert.Equal(t, "v2.0.0", latest)
	assert.True(t, available)
}

func TestCheckQuietlyAlreadyUpToDate(t *testing.T) {
	server := newTestGitHubServer(t, "v1.0.0")
	defer server.Close()

	current, latest, available := checkQuietlyWithVersion(context.Background(), "", "v1.0.0", testFetcher(server.URL))
	assert.Equal(t, "v1.0.0", current)
	assert.Equal(t, "v1.0.0", latest)
	assert.False(t, available)
}

func TestNotifyUpdateNoUpdateAvailable(t *testing.T) {
	server := newTestGitHubServer(t, "v1.0.0")
	defer server.Close()

	var events []output.Event
	sink := output.SinkFunc(func(event output.Event) { events = append(events, event) })

	exit := notifyUpdateWithVersion(context.Background(), sink, NotifyOptions{Mode: CheckModePrompt}, "v1.0.0", testFetcher(server.URL))
	assert.False(t, exit)
	assert.Empty(t, events)
}

func TestNotifyUpdatePromptDisabled(t *testing.T) {
	server := newTestGitHubServer(t, "v2.0.0")
	defer server.Close()

	var events []output.Event
	sink := output.SinkFunc(func(event output.Event) { events = append(events, event) })

	exit := notifyUpdateWithVersion(context.Background(), sink, NotifyOptions{Mode: CheckModeNotify}, "1.0.0", testFetcher(server.URL))
	assert.False(t, exit)
	assert.Len(t, events, 1)
	msg, ok := events[0].(output.MessageEvent)
	assert.True(t, ok)
	assert.Equal(t, output.SeverityNote, msg.Severity)
	assert.Contains(t, msg.Text, "Update available")
}

func TestNotifyUpdatePromptSkip(t *testing.T) {
	server := newTestGitHubServer(t, "v2.0.0")
	defer server.Close()

	var skippedVersion string
	var events []output.Event
	sink := output.SinkFunc(func(event output.Event) {
		events = append(events, event)
		if req, ok := event.(output.UserInputRequestEvent); ok {
			req.ResponseCh() <- output.InputResponse{SelectedKey: "s"}
		}
	})

	exit := notifyUpdateWithVersion(context.Background(), sink, NotifyOptions{
		Mode: CheckModePrompt,
		PersistSkipVersion: func(v string) error {
			skippedVersion = v
			return nil
		},
	}, "1.0.0", testFetcher(server.URL))
	assert.False(t, exit)
	assert.Equal(t, "v2.0.0", skippedVersion)
}

func TestNotifyUpdateSkippedVersionSuppressesPrompt(t *testing.T) {
	server := newTestGitHubServer(t, "v2.0.0")
	defer server.Close()

	var events []output.Event
	sink := output.SinkFunc(func(event output.Event) { events = append(events, event) })

	exit := notifyUpdateWithVersion(context.Background(), sink, NotifyOptions{
		Mode:           CheckModePrompt,
		SkippedVersion: "v2.0.0",
	}, "1.0.0", testFetcher(server.URL))
	assert.False(t, exit)
	assert.Empty(t, events)
}

func TestNotifyUpdatePromptRemind(t *testing.T) {
	server := newTestGitHubServer(t, "v2.0.0")
	defer server.Close()

	var events []output.Event
	sink := output.SinkFunc(func(event output.Event) {
		events = append(events, event)
		if req, ok := event.(output.UserInputRequestEvent); ok {
			req.ResponseCh() <- output.InputResponse{SelectedKey: "r"}
		}
	})

	exit := notifyUpdateWithVersion(context.Background(), sink, NotifyOptions{Mode: CheckModePrompt}, "1.0.0", testFetcher(server.URL))
	assert.False(t, exit)
}

func TestNotifyUpdatePromptCancelled(t *testing.T) {
	server := newTestGitHubServer(t, "v2.0.0")
	defer server.Close()

	var events []output.Event
	sink := output.SinkFunc(func(event output.Event) {
		events = append(events, event)
		if req, ok := event.(output.UserInputRequestEvent); ok {
			assert.Equal(t, "Update lstk to latest version?", req.Prompt())
			assert.Len(t, req.Options(), 3)
			assert.Equal(t, "u", req.Options()[0].Key)
			assert.Equal(t, "r", req.Options()[1].Key)
			assert.Equal(t, "s", req.Options()[2].Key)
			req.ResponseCh() <- output.InputResponse{Cancelled: true}
		}
	})

	exit := notifyUpdateWithVersion(context.Background(), sink, NotifyOptions{Mode: CheckModePrompt}, "1.0.0", testFetcher(server.URL))
	assert.False(t, exit)
}

func TestNotifyUpdateOffMakesNoRequest(t *testing.T) {
	var events []output.Event
	sink := output.SinkFunc(func(event output.Event) { events = append(events, event) })

	exit := notifyUpdateWithVersion(context.Background(), sink, NotifyOptions{Mode: CheckModeOff}, "1.0.0", failingFetcher(t))
	assert.False(t, exit)
	assert.Empty(t, events)
}

// A zero-value Mode must never block on input: domain code reached without a
// resolved policy falls back to the non-blocking note.
func TestNotifyUpdateZeroModeDoesNotPrompt(t *testing.T) {
	server := newTestGitHubServer(t, "v2.0.0")
	defer server.Close()

	var events []output.Event
	sink := output.SinkFunc(func(event output.Event) { events = append(events, event) })

	exit := notifyUpdateWithVersion(context.Background(), sink, NotifyOptions{}, "1.0.0", testFetcher(server.URL))
	assert.False(t, exit)
	assert.Len(t, events, 1)
	msg, ok := events[0].(output.MessageEvent)
	assert.True(t, ok)
	assert.Equal(t, "Update available: 1.0.0 → v2.0.0 (run lstk update)", msg.Text)
}

func TestNotifyUpdateNotifyLineNamesExternalManager(t *testing.T) {
	server := newTestGitHubServer(t, "v2.0.0")
	defer server.Close()

	tests := []struct {
		manager ExternalManager
		want    string
	}{
		{ManagerMise, "Update available: 1.0.0 → v2.0.0 (installed with mise — run mise upgrade lstk)"},
		{ManagerNix, "Update available: 1.0.0 → v2.0.0 (installed with Nix — update it with Nix)"},
		{ManagerScoop, "Update available: 1.0.0 → v2.0.0 (installed with Scoop — run scoop update lstk)"},
	}

	for _, tt := range tests {
		t.Run(string(tt.manager), func(t *testing.T) {
			var events []output.Event
			sink := output.SinkFunc(func(event output.Event) { events = append(events, event) })

			exit := notifyUpdateWithVersion(context.Background(), sink, NotifyOptions{
				Mode:    CheckModeNotify,
				Manager: tt.manager,
			}, "1.0.0", testFetcher(server.URL))
			assert.False(t, exit)
			assert.Len(t, events, 1)
			msg, ok := events[0].(output.MessageEvent)
			assert.True(t, ok)
			assert.Equal(t, tt.want, msg.Text)
		})
	}
}

// The "Don't ask again" option writes to the config file, so it is withheld
// when there is nothing to write to (a genuine first run, where creating the
// config here would suppress the emulator picker).
func TestNotifyUpdateHidesDontAskAgainWithoutPersist(t *testing.T) {
	server := newTestGitHubServer(t, "v2.0.0")
	defer server.Close()

	var options []output.InputOption
	sink := output.SinkFunc(func(event output.Event) {
		if req, ok := event.(output.UserInputRequestEvent); ok {
			options = req.Options()
			req.ResponseCh() <- output.InputResponse{SelectedKey: "r"}
		}
	})

	notifyUpdateWithVersion(context.Background(), sink, NotifyOptions{Mode: CheckModePrompt}, "1.0.0", testFetcher(server.URL))
	assert.Len(t, options, 3)
	for _, opt := range options {
		assert.NotEqual(t, "n", opt.Key)
	}
}

func TestNotifyUpdateDontAskAgainPersistsNotify(t *testing.T) {
	server := newTestGitHubServer(t, "v2.0.0")
	defer server.Close()

	var persisted CheckMode
	var events []output.Event
	var options []output.InputOption
	sink := output.SinkFunc(func(event output.Event) {
		events = append(events, event)
		if req, ok := event.(output.UserInputRequestEvent); ok {
			options = req.Options()
			req.ResponseCh() <- output.InputResponse{SelectedKey: "n"}
		}
	})

	exit := notifyUpdateWithVersion(context.Background(), sink, NotifyOptions{
		Mode:             CheckModePrompt,
		ConfigPath:       "/home/me/.config/lstk/config.toml",
		PersistCheckMode: func(mode CheckMode) error { persisted = mode; return nil },
	}, "1.0.0", testFetcher(server.URL))

	assert.False(t, exit)
	assert.Len(t, options, 4)
	assert.Equal(t, "n", options[3].Key)
	assert.Equal(t, "Don't ask again", options[3].Label)
	assert.Equal(t, CheckModeNotify, persisted)

	var texts []string
	for _, event := range events {
		if msg, ok := event.(output.MessageEvent); ok {
			texts = append(texts, msg.Text)
		}
	}
	assert.Contains(t, texts, `Won't ask again — saved update_check = "notify" to /home/me/.config/lstk/config.toml`)
}

func TestNotifyUpdateDontAskAgainPersistFailureWarns(t *testing.T) {
	server := newTestGitHubServer(t, "v2.0.0")
	defer server.Close()

	var warnings []string
	sink := output.SinkFunc(func(event output.Event) {
		if msg, ok := event.(output.MessageEvent); ok && msg.Severity == output.SeverityWarning {
			warnings = append(warnings, msg.Text)
		}
		if req, ok := event.(output.UserInputRequestEvent); ok {
			req.ResponseCh() <- output.InputResponse{SelectedKey: "n"}
		}
	})

	exit := notifyUpdateWithVersion(context.Background(), sink, NotifyOptions{
		Mode:             CheckModePrompt,
		PersistCheckMode: func(mode CheckMode) error { return fmt.Errorf("read-only file system") },
	}, "1.0.0", testFetcher(server.URL))

	assert.False(t, exit)
	assert.Contains(t, warnings, "Failed to save update check preference: read-only file system")
}
