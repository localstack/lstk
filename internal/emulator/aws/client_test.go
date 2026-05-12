package aws

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
			_, _ = fmt.Fprintln(w, `{"version": "4.14.1", "services": {}}`)
		}))
		defer server.Close()

		c := NewClient()
		version, err := c.FetchVersion(context.Background(), server.Listener.Addr().String())
		require.NoError(t, err)
		assert.Equal(t, "4.14.1", version)
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

func TestFetchResources(t *testing.T) {
	t.Parallel()

	t.Run("returns flat rows sorted by service then resource", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/x-ndjson")
			_, _ = fmt.Fprintln(w, `{"AWS::S3::Bucket": [{"region_name": "us-east-1", "account_id": "000000000000", "id": "my-bucket"}]}`)
			_, _ = fmt.Fprintln(w, `{"AWS::Lambda::Function": [{"region_name": "us-east-1", "account_id": "000000000000", "id": "my-function"}]}`)
		}))
		defer server.Close()

		c := NewClient()
		rows, err := c.FetchResources(context.Background(), server.Listener.Addr().String())
		require.NoError(t, err)
		require.Len(t, rows, 2)
		assert.Equal(t, "Lambda", rows[0].Service)
		assert.Equal(t, "my-function", rows[0].Name)
		assert.Equal(t, "us-east-1", rows[0].Region)
		assert.Equal(t, "000000000000", rows[0].Account)
		assert.Equal(t, "S3", rows[1].Service)
		assert.Equal(t, "my-bucket", rows[1].Name)
	})

	t.Run("extracts name from ARN", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/x-ndjson")
			_, _ = fmt.Fprintln(w, `{"AWS::SNS::Topic": [{"region_name": "us-east-1", "account_id": "000000000000", "id": "arn:aws:sns:us-east-1:000000000000:my-topic"}]}`)
		}))
		defer server.Close()

		c := NewClient()
		rows, err := c.FetchResources(context.Background(), server.Listener.Addr().String())
		require.NoError(t, err)
		require.Len(t, rows, 1)
		assert.Equal(t, "my-topic", rows[0].Name)
	})

	t.Run("returns empty slice when no resources", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/x-ndjson")
		}))
		defer server.Close()

		c := NewClient()
		rows, err := c.FetchResources(context.Background(), server.Listener.Addr().String())
		require.NoError(t, err)
		assert.Empty(t, rows)
	})

	t.Run("returns error on non-200", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		c := NewClient()
		_, err := c.FetchResources(context.Background(), server.Listener.Addr().String())
		require.Error(t, err)
	})
}

func TestExportState(t *testing.T) {
	t.Parallel()

	t.Run("streams body on 200", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/_localstack/pods/state", r.URL.Path)
			assert.Equal(t, http.MethodGet, r.Method)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ZIP_DATA"))
		}))
		defer srv.Close()

		var buf bytes.Buffer
		c := NewClient()
		err := c.ExportState(context.Background(), srv.Listener.Addr().String(), &buf)
		require.NoError(t, err)
		assert.Equal(t, "ZIP_DATA", buf.String())
	})

	t.Run("returns error on 500", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		c := NewClient()
		err := c.ExportState(context.Background(), srv.Listener.Addr().String(), io.Discard)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "500")
	})

	t.Run("returns error on 404", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		c := NewClient()
		err := c.ExportState(context.Background(), srv.Listener.Addr().String(), io.Discard)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "404")
	})

	t.Run("returns error on connection refused", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
		addr := srv.Listener.Addr().String()
		srv.Close()

		c := NewClient()
		err := c.ExportState(context.Background(), addr, io.Discard)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "connect to LocalStack")
	})

	t.Run("returns error on context cancellation", func(t *testing.T) {
		t.Parallel()
		started := make(chan struct{})
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(started)
			<-r.Context().Done()
		}))
		defer srv.Close()

		ctx, cancel := context.WithCancel(context.Background())
		c := NewClient()

		errCh := make(chan error, 1)
		go func() {
			errCh <- c.ExportState(ctx, srv.Listener.Addr().String(), io.Discard)
		}()

		<-started
		cancel()

		err := <-errCh
		require.Error(t, err)
	})

	t.Run("handles large body", func(t *testing.T) {
		t.Parallel()
		const size = 1 << 20 // 1 MB
		payload := strings.Repeat("X", size)

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(payload))
		}))
		defer srv.Close()

		var buf bytes.Buffer
		c := NewClient()
		err := c.ExportState(context.Background(), srv.Listener.Addr().String(), &buf)
		require.NoError(t, err)
		assert.Equal(t, size, buf.Len())
	})
}

