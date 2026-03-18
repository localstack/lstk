package update

import (
	"context"
	"encoding/json"
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

func withLatestReleaseURL(t *testing.T, url string) func() {
	t.Helper()
	orig := latestReleaseURL
	latestReleaseURL = url
	return func() { latestReleaseURL = orig }
}

func TestCheckQuietlyDevBuild(t *testing.T) {
	current, latest, available := CheckQuietly(context.Background(), "")
	assert.Equal(t, "dev", current)
	assert.Empty(t, latest)
	assert.False(t, available)
}

func TestCheckQuietlyNetworkError(t *testing.T) {
	cleanup := withLatestReleaseURL(t, "http://localhost:1/nonexistent")
	defer cleanup()

	current, latest, available := checkQuietlyWithVersion(context.Background(), "", "1.0.0")
	assert.Equal(t, "1.0.0", current)
	assert.Empty(t, latest)
	assert.False(t, available)
}

func TestCheckQuietlyUpdateAvailable(t *testing.T) {
	server := newTestGitHubServer(t, "v2.0.0")
	defer server.Close()
	cleanup := withLatestReleaseURL(t, server.URL)
	defer cleanup()

	current, latest, available := checkQuietlyWithVersion(context.Background(), "", "1.0.0")
	assert.Equal(t, "1.0.0", current)
	assert.Equal(t, "v2.0.0", latest)
	assert.True(t, available)
}

func TestCheckQuietlyAlreadyUpToDate(t *testing.T) {
	server := newTestGitHubServer(t, "v1.0.0")
	defer server.Close()
	cleanup := withLatestReleaseURL(t, server.URL)
	defer cleanup()

	current, latest, available := checkQuietlyWithVersion(context.Background(), "", "v1.0.0")
	assert.Equal(t, "v1.0.0", current)
	assert.Equal(t, "v1.0.0", latest)
	assert.False(t, available)
}

func TestUpdateCommandHint(t *testing.T) {
	tests := []struct {
		method InstallMethod
		want   string
	}{
		{InstallHomebrew, "brew upgrade localstack/tap/lstk"},
		{InstallNPM, "npm install -g @localstack/lstk@latest"},
		{InstallBinary, "lstk update"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, UpdateCommandHint(InstallInfo{Method: tt.method}))
	}
}

func TestNotifyUpdateNoUpdateAvailable(t *testing.T) {
	server := newTestGitHubServer(t, "v1.0.0")
	defer server.Close()
	cleanup := withLatestReleaseURL(t, server.URL)
	defer cleanup()

	var events []any
	sink := output.SinkFunc(func(event any) { events = append(events, event) })

	exit := notifyUpdateWithVersion(context.Background(), sink, "", true, nil, "v1.0.0")
	assert.False(t, exit)
	assert.Empty(t, events)
}

func TestNotifyUpdatePromptDisabled(t *testing.T) {
	server := newTestGitHubServer(t, "v2.0.0")
	defer server.Close()
	cleanup := withLatestReleaseURL(t, server.URL)
	defer cleanup()

	var events []any
	sink := output.SinkFunc(func(event any) { events = append(events, event) })

	exit := notifyUpdateWithVersion(context.Background(), sink, "", false, nil, "1.0.0")
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
	cleanup := withLatestReleaseURL(t, server.URL)
	defer cleanup()

	var events []any
	sink := output.SinkFunc(func(event any) {
		events = append(events, event)
		if req, ok := event.(output.UserInputRequestEvent); ok {
			req.ResponseCh <- output.InputResponse{SelectedKey: "s"}
		}
	})

	exit := notifyUpdateWithVersion(context.Background(), sink, "", true, nil, "1.0.0")
	assert.False(t, exit)
}

func TestNotifyUpdatePromptNever(t *testing.T) {
	server := newTestGitHubServer(t, "v2.0.0")
	defer server.Close()
	cleanup := withLatestReleaseURL(t, server.URL)
	defer cleanup()

	persistCalled := false

	var events []any
	sink := output.SinkFunc(func(event any) {
		events = append(events, event)
		if req, ok := event.(output.UserInputRequestEvent); ok {
			req.ResponseCh <- output.InputResponse{SelectedKey: "n"}
		}
	})

	exit := notifyUpdateWithVersion(context.Background(), sink, "", true, func() error {
		persistCalled = true
		return nil
	}, "1.0.0")
	assert.False(t, exit)
	assert.True(t, persistCalled)
}

func TestNotifyUpdatePromptCancelled(t *testing.T) {
	server := newTestGitHubServer(t, "v2.0.0")
	defer server.Close()
	cleanup := withLatestReleaseURL(t, server.URL)
	defer cleanup()

	var events []any
	sink := output.SinkFunc(func(event any) {
		events = append(events, event)
		if req, ok := event.(output.UserInputRequestEvent); ok {
			req.ResponseCh <- output.InputResponse{Cancelled: true}
		}
	})

	exit := notifyUpdateWithVersion(context.Background(), sink, "", true, nil, "1.0.0")
	assert.False(t, exit)
}
