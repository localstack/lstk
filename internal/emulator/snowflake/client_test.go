package snowflake

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchVersion(t *testing.T) {
	t.Parallel()

	t.Run("returns version from health endpoint", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/_localstack/health", r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintln(w, `{"version": "2026.5.0"}`)
		}))
		defer server.Close()

		c := NewClient()
		version, err := c.FetchVersion(context.Background(), server.Listener.Addr().String())
		require.NoError(t, err)
		assert.Equal(t, "2026.5.0", version)
	})

	t.Run("returns error on non-200", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		c := NewClient()
		_, err := c.FetchVersion(context.Background(), server.Listener.Addr().String())
		require.Error(t, err)
	})
}

func TestFetchResources_AlwaysEmpty(t *testing.T) {
	t.Parallel()
	c := NewClient()
	rows, err := c.FetchResources(context.Background(), "unused")
	require.NoError(t, err)
	assert.Empty(t, rows)
}
