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

func TestCheckQuietlyDevBuild(t *testing.T) {
	current, latest, available := CheckQuietly(context.Background(), "")
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

	var events []any
	sink := output.SinkFunc(func(event any) { events = append(events, event) })

	exit := notifyUpdateWithVersion(context.Background(), sink, NotifyOptions{UpdatePrompt: true}, "v1.0.0", testFetcher(server.URL))
	assert.False(t, exit)
	assert.Empty(t, events)
}

func TestNotifyUpdatePromptDisabled(t *testing.T) {
	server := newTestGitHubServer(t, "v2.0.0")
	defer server.Close()

	var events []any
	sink := output.SinkFunc(func(event any) { events = append(events, event) })

	exit := notifyUpdateWithVersion(context.Background(), sink, NotifyOptions{}, "1.0.0", testFetcher(server.URL))
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
	var events []any
	sink := output.SinkFunc(func(event any) {
		events = append(events, event)
		if req, ok := event.(output.UserInputRequestEvent); ok {
			req.ResponseCh <- output.InputResponse{SelectedKey: "s"}
		}
	})

	exit := notifyUpdateWithVersion(context.Background(), sink, NotifyOptions{
		UpdatePrompt: true,
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

	var events []any
	sink := output.SinkFunc(func(event any) { events = append(events, event) })

	exit := notifyUpdateWithVersion(context.Background(), sink, NotifyOptions{
		UpdatePrompt:   true,
		SkippedVersion: "v2.0.0",
	}, "1.0.0", testFetcher(server.URL))
	assert.False(t, exit)
	assert.Empty(t, events)
}

func TestNotifyUpdatePromptRemind(t *testing.T) {
	server := newTestGitHubServer(t, "v2.0.0")
	defer server.Close()

	var events []any
	sink := output.SinkFunc(func(event any) {
		events = append(events, event)
		if req, ok := event.(output.UserInputRequestEvent); ok {
			req.ResponseCh <- output.InputResponse{SelectedKey: "r"}
		}
	})

	exit := notifyUpdateWithVersion(context.Background(), sink, NotifyOptions{UpdatePrompt: true}, "1.0.0", testFetcher(server.URL))
	assert.False(t, exit)
}

func TestNotifyUpdatePromptCancelled(t *testing.T) {
	server := newTestGitHubServer(t, "v2.0.0")
	defer server.Close()

	var events []any
	sink := output.SinkFunc(func(event any) {
		events = append(events, event)
		if req, ok := event.(output.UserInputRequestEvent); ok {
			assert.Equal(t, "Update lstk to latest version?", req.Prompt)
			assert.Len(t, req.Options, 3)
			assert.Equal(t, "u", req.Options[0].Key)
			assert.Equal(t, "r", req.Options[1].Key)
			assert.Equal(t, "s", req.Options[2].Key)
			req.ResponseCh <- output.InputResponse{Cancelled: true}
		}
	})

	exit := notifyUpdateWithVersion(context.Background(), sink, NotifyOptions{UpdatePrompt: true}, "1.0.0", testFetcher(server.URL))
	assert.False(t, exit)
}

