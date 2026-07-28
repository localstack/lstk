package aws

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/localstack/lstk/internal/snapshot"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type podCall struct {
	name string
	call func(c *Client, host string) error
}

// podCalls returns the emulator-backed pod operations that authenticate with the
// caller's token when there is one and fall back to the emulator's own identity
// when there is not. Every operation below is called without a token.
func podCalls() []podCall {
	return []podCall{
		{"save", func(c *Client, host string) error {
			_, err := c.SavePodSnapshot(context.Background(), host, "my-pod", "", nil)
			return err
		}},
		{"load", func(c *Client, host string) error {
			_, err := c.LoadPodSnapshot(context.Background(), host, "my-pod", 0, "", "")
			return err
		}},
		{"diff", func(c *Client, host string) error {
			_, err := c.DiffPodSnapshot(context.Background(), host, "my-pod", 0, "")
			return err
		}},
		{"remove", func(c *Client, host string) error {
			return c.RemovePodSnapshot(context.Background(), host, "my-pod", "")
		}},
	}
}

// An omitted token must leave the Authorization header off the request entirely,
// so the emulator falls back to the identity it was started with instead of
// receiving empty credentials.
func TestPodRequests_OmitAuthorizationHeaderWithoutToken(t *testing.T) {
	t.Parallel()
	for _, tc := range podCalls() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var hasAuthHeader bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, hasAuthHeader = r.Header["Authorization"]
				if strings.HasSuffix(r.URL.Path, "/diff") {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{}`))
					return
				}
				w.Header().Set("Content-Type", "application/x-ndjson")
				_, _ = w.Write([]byte(`{"event":"completion","status":"ok","info":{"version":1}}` + "\n"))
			}))
			defer server.Close()

			require.NoError(t, tc.call(NewClient(), server.URL))
			assert.False(t, hasAuthHeader, "Authorization header should be omitted when no token is supplied")
		})
	}
}

// A rejection for lack of a usable identity must surface as ErrAuthRequired so
// the snapshot layer can render an actionable message.
func TestPodRequests_MapUnauthorizedToErrAuthRequired(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		for _, tc := range podCalls() {
			t.Run(http.StatusText(status)+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(status)
					_, _ = w.Write([]byte("no credentials configured"))
				}))
				defer server.Close()

				err := tc.call(NewClient(), server.URL)
				require.Error(t, err)
				assert.True(t, errors.Is(err, snapshot.ErrAuthRequired), "got %v", err)
			})
		}
	}
}
