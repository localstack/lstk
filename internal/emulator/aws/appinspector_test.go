package aws

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/localstack/lstk/internal/inspect"
)

func appInspectorServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, appInspectorStatusPath, r.URL.Path)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestAppInspectorEnabled(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		body string
		want bool
	}{
		{`{"status": "ENABLED", "note": "", "epoch": "a3db29c4"}`, true},
		{`{"status": "DISABLED", "note": "", "epoch": null}`, false},
	} {
		srv := appInspectorServer(t, http.StatusOK, tc.body)

		got, err := NewClient().AppInspectorEnabled(context.Background(), srv.URL)

		require.NoError(t, err)
		assert.Equal(t, tc.want, got)
	}
}

func TestSetAppInspectorEnabled(t *testing.T) {
	t.Parallel()
	var gotBody, gotMethod, gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody, gotMethod, gotContentType = string(b), r.Method, r.Header.Get("Content-Type")
		_, _ = io.WriteString(w, `{"status": "DISABLED", "changed": true}`)
	}))
	t.Cleanup(srv.Close)

	changed, err := NewClient().SetAppInspectorEnabled(context.Background(), srv.URL+"/", false)

	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, http.MethodPut, gotMethod)
	assert.Equal(t, "application/json", gotContentType)
	assert.JSONEq(t, `{"status": "disabled"}`, gotBody)
}

func TestAppInspectorErrors(t *testing.T) {
	t.Parallel()

	t.Run("404 means the emulator has no App Inspector", func(t *testing.T) {
		t.Parallel()
		srv := appInspectorServer(t, http.StatusNotFound, "")

		_, err := NewClient().AppInspectorEnabled(context.Background(), srv.URL)

		assert.ErrorIs(t, err, inspect.ErrUnavailable)
	})

	t.Run("error body is shown without its JSON wrapper", func(t *testing.T) {
		t.Parallel()
		srv := appInspectorServer(t, http.StatusInternalServerError, `{"error": "Failed to change AppInspector status"}`)

		_, err := NewClient().SetAppInspectorEnabled(context.Background(), srv.URL, true)

		assert.EqualError(t, err, "LocalStack returned status 500: Failed to change AppInspector status")
	})

	t.Run("state that does not match the request", func(t *testing.T) {
		t.Parallel()
		srv := appInspectorServer(t, http.StatusOK, `{"status": "DISABLED", "changed": false}`)

		_, err := NewClient().SetAppInspectorEnabled(context.Background(), srv.URL, true)

		assert.Error(t, err)
	})

	t.Run("unknown status value", func(t *testing.T) {
		t.Parallel()
		srv := appInspectorServer(t, http.StatusOK, `{"status": "PAUSED"}`)

		_, err := NewClient().AppInspectorEnabled(context.Background(), srv.URL)

		assert.Error(t, err)
	})
}
