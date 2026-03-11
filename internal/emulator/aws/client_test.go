package aws

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
			_, _ = fmt.Fprintln(w, `{"version": "4.14.1", "services": {}}`)
		}))
		defer server.Close()

		c := NewClient(nil)
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

		c := NewClient(nil)
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

		c := NewClient(nil)
		rows, err := c.FetchResources(context.Background(), server.Listener.Addr().String())
		require.NoError(t, err)
		require.Len(t, rows, 2)
		assert.Equal(t, "Lambda", rows[0].Service)
		assert.Equal(t, "my-function", rows[0].Resource)
		assert.Equal(t, "us-east-1", rows[0].Region)
		assert.Equal(t, "000000000000", rows[0].Account)
		assert.Equal(t, "S3", rows[1].Service)
		assert.Equal(t, "my-bucket", rows[1].Resource)
	})

	t.Run("extracts name from ARN", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/x-ndjson")
			_, _ = fmt.Fprintln(w, `{"AWS::SNS::Topic": [{"region_name": "us-east-1", "account_id": "000000000000", "id": "arn:aws:sns:us-east-1:000000000000:my-topic"}]}`)
		}))
		defer server.Close()

		c := NewClient(nil)
		rows, err := c.FetchResources(context.Background(), server.Listener.Addr().String())
		require.NoError(t, err)
		require.Len(t, rows, 1)
		assert.Equal(t, "my-topic", rows[0].Resource)
	})

	t.Run("returns empty slice when no resources", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/x-ndjson")
		}))
		defer server.Close()

		c := NewClient(nil)
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

		c := NewClient(nil)
		_, err := c.FetchResources(context.Background(), server.Listener.Addr().String())
		require.Error(t, err)
	})
}

func TestExtractResourceName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{"my-bucket", "my-bucket"},
		{"arn:aws:sns:us-east-1:000000000000:my-topic", "my-topic"},
		{"arn:aws:iam::000000000000:role/my-role", "my-role"},
		{"arn:aws:lambda:us-east-1:000000000000:function:my-func", "my-func"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, extractResourceName(tt.input), "extractResourceName(%q)", tt.input)
	}
}
