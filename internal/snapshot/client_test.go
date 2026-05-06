package snapshot_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/localstack/lstk/internal/snapshot"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStateClient_ExportState_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/_localstack/pods/state", r.URL.Path)
		assert.Equal(t, http.MethodGet, r.Method)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ZIP_DATA"))
	}))
	defer srv.Close()

	client := snapshot.NewStateClient(srv.URL)
	body, err := client.ExportState(context.Background())
	require.NoError(t, err)
	defer func() { _ = body.Close() }()

	data, err := io.ReadAll(body)
	require.NoError(t, err)
	assert.Equal(t, "ZIP_DATA", string(data))
}

func TestStateClient_ExportState_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := snapshot.NewStateClient(srv.URL)
	_, err := client.ExportState(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestStateClient_ExportState_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := snapshot.NewStateClient(srv.URL)
	_, err := client.ExportState(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

func TestStateClient_ExportState_ConnectionRefused(t *testing.T) {
	// Bind then immediately close to get a port that refuses connections.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	addr := srv.URL
	srv.Close()

	client := snapshot.NewStateClient(addr)
	_, err := client.ExportState(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connect to LocalStack")
}

func TestStateClient_ExportState_ContextCancelled(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		// block until the client cancels
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	client := snapshot.NewStateClient(srv.URL)

	errCh := make(chan error, 1)
	go func() {
		_, err := client.ExportState(ctx)
		errCh <- err
	}()

	<-started
	cancel()

	err := <-errCh
	require.Error(t, err)
}

func TestStateClient_ExportState_LargeBody(t *testing.T) {
	const size = 1 << 20 // 1 MB
	payload := strings.Repeat("X", size)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()

	client := snapshot.NewStateClient(srv.URL)
	body, err := client.ExportState(context.Background())
	require.NoError(t, err)
	defer func() { _ = body.Close() }()

	data, err := io.ReadAll(body)
	require.NoError(t, err)
	assert.Equal(t, size, len(data))
}
