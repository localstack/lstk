package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFeedbackCommandPromptsInInteractiveMode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/feedback", r.URL.Path)
		require.True(t, strings.HasPrefix(r.Header.Get("Authorization"), "Basic "))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		var payload struct {
			Message  string         `json:"message"`
			Metadata map[string]any `json:"metadata"`
		}
		require.NoError(t, json.Unmarshal(body, &payload))
		require.Equal(t, "The login flow should mention keyring precedence", payload.Message)
		require.Equal(t, "Configured", payload.Metadata["auth"])
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	binPath, err := filepath.Abs(binaryPath())
	require.NoError(t, err)

	cmd := exec.CommandContext(ctx, binPath, "feedback")
	cmd.Env = env.Without(env.APIEndpoint, env.AuthToken).
		With(env.APIEndpoint, srv.URL).
		With(env.AuthToken, "auth-token").
		With(env.TermProgram, "ghostty")

	ptmx, err := pty.Start(cmd)
	require.NoError(t, err)
	defer func() { _ = ptmx.Close() }()

	out := &syncBuffer{}
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(out, ptmx)
		close(done)
	}()

	require.Eventually(t, func() bool {
		return strings.Contains(out.String(), "What's your feedback?")
	}, 10*time.Second, 100*time.Millisecond)

	_, err = ptmx.Write([]byte("The login flow should mention keyring precedence\n"))
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return strings.Contains(out.String(), "This report will include:")
	}, 10*time.Second, 100*time.Millisecond)

	assert.Contains(t, out.String(), "Press enter to submit or esc to cancel")
	assert.Contains(t, out.String(), "Feedback: The login flow should mention keyring precedence")
	assert.Contains(t, out.String(), "Version (lstk):")
	assert.Contains(t, out.String(), "OS (arch): darwin")
	assert.Contains(t, out.String(), "Installation:")
	assert.Contains(t, out.String(), "Shell:")
	assert.Contains(t, out.String(), "Container runtime:")
	assert.Contains(t, out.String(), "Auth:")
	assert.Contains(t, out.String(), "Config:")
	assert.Contains(t, out.String(), "Confirm submitting this feedback?")
	assert.Contains(t, out.String(), "[Y/n]")

	_, err = ptmx.Write([]byte("\r"))
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return strings.Contains(out.String(), "Thank you for your feedback!")
	}, 10*time.Second, 100*time.Millisecond)
	assert.Contains(t, out.String(), "Submitting feedback")
	assert.NotContains(t, out.String(), "Feedback ID:")

	require.NoError(t, cmd.Wait())
	_ = ptmx.Close()
	<-done
}
