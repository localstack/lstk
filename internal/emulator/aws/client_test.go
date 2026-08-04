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

	"github.com/localstack/lstk/internal/snapshot"
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
		version, err := c.FetchVersion(context.Background(), server.URL)
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
		_, err := c.FetchVersion(context.Background(), server.URL)
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
		rows, err := c.FetchResources(context.Background(), server.URL)
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
		rows, err := c.FetchResources(context.Background(), server.URL)
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
		rows, err := c.FetchResources(context.Background(), server.URL)
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
		_, err := c.FetchResources(context.Background(), server.URL)
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
		_, err := c.ExportState(context.Background(), srv.URL, nil, &buf)
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
		_, err := c.ExportState(context.Background(), srv.URL, nil, io.Discard)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "500")
	})

	t.Run("translates an empty-body 404 into the feature-unavailable sentinel", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		c := NewClient()
		_, err := c.ExportState(context.Background(), srv.URL, nil, io.Discard)
		require.ErrorIs(t, err, snapshot.ErrSnapshotFeatureUnavailable)
	})

	t.Run("keeps a 404 that carries a body as a generic error", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("no such route"))
		}))
		defer srv.Close()

		c := NewClient()
		_, err := c.ExportState(context.Background(), srv.URL, nil, io.Discard)
		require.Error(t, err)
		assert.NotErrorIs(t, err, snapshot.ErrSnapshotFeatureUnavailable)
		assert.Contains(t, err.Error(), "404")
		assert.Contains(t, err.Error(), "no such route")
	})

	t.Run("returns error on connection refused", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
		addr := srv.URL
		srv.Close()

		c := NewClient()
		_, err := c.ExportState(context.Background(), addr, nil, io.Discard)
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
			_, exportErr := c.ExportState(ctx, srv.URL, nil, io.Discard)
			errCh <- exportErr
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
		_, err := c.ExportState(context.Background(), srv.URL, nil, &buf)
		require.NoError(t, err)
		assert.Equal(t, size, buf.Len())
	})

	t.Run("sends services query param and parses extracted services header", func(t *testing.T) {
		t.Parallel()
		var gotQuery string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotQuery = r.URL.RawQuery
			w.Header().Set("x-localstack-pod-services", "s3,dynamodb")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ZIP_DATA"))
		}))
		defer srv.Close()

		var buf bytes.Buffer
		c := NewClient()
		extracted, err := c.ExportState(context.Background(), srv.URL, []string{"s3", "dynamodb"}, &buf)
		require.NoError(t, err)
		assert.Equal(t, "services=s3,dynamodb", gotQuery)
		assert.Equal(t, []string{"s3", "dynamodb"}, extracted)
	})

	t.Run("no services filter omits query param and extracted list", func(t *testing.T) {
		t.Parallel()
		var gotQuery string
		var hadQuery bool
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotQuery = r.URL.RawQuery
			hadQuery = r.URL.Query().Has("services")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ZIP_DATA"))
		}))
		defer srv.Close()

		var buf bytes.Buffer
		c := NewClient()
		extracted, err := c.ExportState(context.Background(), srv.URL, nil, &buf)
		require.NoError(t, err)
		assert.Equal(t, "", gotQuery)
		assert.False(t, hadQuery)
		assert.Nil(t, extracted)
	})
}

func TestResetState(t *testing.T) {
	t.Parallel()

	t.Run("posts to state reset endpoint on 200", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/_localstack/state/reset", r.URL.Path)
			assert.Equal(t, http.MethodPost, r.Method)
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		c := NewClient()
		err := c.ResetState(context.Background(), srv.URL)
		require.NoError(t, err)
	})

	t.Run("returns error on 500", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		c := NewClient()
		err := c.ResetState(context.Background(), srv.URL)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "500")
	})

	t.Run("translates an empty-body 404 into the feature-unavailable sentinel", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		c := NewClient()
		err := c.ResetState(context.Background(), srv.URL)
		require.ErrorIs(t, err, snapshot.ErrSnapshotFeatureUnavailable)
	})

	t.Run("keeps a 404 that carries a body as a generic error", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("no such route"))
		}))
		defer srv.Close()

		c := NewClient()
		err := c.ResetState(context.Background(), srv.URL)
		require.Error(t, err)
		assert.NotErrorIs(t, err, snapshot.ErrSnapshotFeatureUnavailable)
		assert.Contains(t, err.Error(), "404")
	})

	t.Run("returns error on connection refused", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
		addr := srv.URL
		srv.Close()

		c := NewClient()
		err := c.ResetState(context.Background(), addr)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "connect to LocalStack")
	})

	t.Run("returns error on context cancellation", func(t *testing.T) {
		t.Parallel()
		started := make(chan struct{})
		srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			close(started)
			<-r.Context().Done()
		}))
		defer srv.Close()

		ctx, cancel := context.WithCancel(context.Background())
		c := NewClient()

		errCh := make(chan error, 1)
		go func() {
			errCh <- c.ResetState(ctx, srv.URL)
		}()

		<-started
		cancel()

		err := <-errCh
		require.Error(t, err)
	})
}


// snapshotOps invokes every snapshot-related endpoint the emulator gates behind
// the Cloud Pods license, so the empty-body-404 translation is asserted for all
// of them at once. A method missing from this table is a method that would still
// surface the raw "status 404" error (DEVX-1009).
func snapshotOps() map[string]func(context.Context, *Client, string) error {
	return map[string]func(context.Context, *Client, string) error{
		"ExportState": func(ctx context.Context, c *Client, host string) error {
			_, err := c.ExportState(ctx, host, nil, io.Discard)
			return err
		},
		"ImportState": func(ctx context.Context, c *Client, host string) error {
			return c.ImportState(ctx, host, strings.NewReader("zip"), "")
		},
		"ResetState": func(ctx context.Context, c *Client, host string) error {
			return c.ResetState(ctx, host)
		},
		"DiffPodSnapshot": func(ctx context.Context, c *Client, host string) error {
			_, err := c.DiffPodSnapshot(ctx, host, "pod", 0, "tok")
			return err
		},
		"RemovePodSnapshot": func(ctx context.Context, c *Client, host string) error {
			return c.RemovePodSnapshot(ctx, host, "pod", "tok")
		},
		"SavePodSnapshot": func(ctx context.Context, c *Client, host string) error {
			_, err := c.SavePodSnapshot(ctx, host, "pod", "tok", nil)
			return err
		},
		"LoadPodSnapshot": func(ctx context.Context, c *Client, host string) error {
			_, err := c.LoadPodSnapshot(ctx, host, "pod", 0, "tok", "")
			return err
		},
		"RegisterRemote": func(ctx context.Context, c *Client, host string) error {
			return c.RegisterRemote(ctx, host, "remote", "s3://bucket/prefix")
		},
		"ListPodsRemote": func(ctx context.Context, c *Client, host string) error {
			_, err := c.ListPodsRemote(ctx, host, "remote", nil, "tok", "")
			return err
		},
		"SavePodRemote": func(ctx context.Context, c *Client, host string) error {
			_, err := c.SavePodRemote(ctx, host, "pod", "remote", nil, "tok", nil)
			return err
		},
		"LoadPodRemote": func(ctx context.Context, c *Client, host string) error {
			_, err := c.LoadPodRemote(ctx, host, "pod", "remote", nil, "tok", "")
			return err
		},
	}
}

// An unentitled emulator never registers the pods routes, so every one of them
// falls through to the router's bare 404 with no body.
func TestSnapshotEndpointsTranslateEmptyBody404(t *testing.T) {
	t.Parallel()

	for name, invoke := range snapshotOps() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			}))
			defer srv.Close()

			err := invoke(context.Background(), NewClient(), srv.URL)
			require.ErrorIs(t, err, snapshot.ErrSnapshotFeatureUnavailable)
		})
	}
}

// The discriminator must stay narrow: a 404 carrying a message is a real error
// from a route that does exist, not a licensing verdict.
func TestSnapshotEndpointsKeep404WithBodyGeneric(t *testing.T) {
	t.Parallel()

	for name, invoke := range snapshotOps() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte("something specific went wrong"))
			}))
			defer srv.Close()

			err := invoke(context.Background(), NewClient(), srv.URL)
			require.Error(t, err)
			assert.NotErrorIs(t, err, snapshot.ErrSnapshotFeatureUnavailable)
		})
	}
}

func TestEmulatorStatusError(t *testing.T) {
	t.Parallel()

	t.Run("omits the body segment when the body is empty", func(t *testing.T) {
		t.Parallel()
		err := emulatorStatusError("LocalStack returned status 404", nil)
		require.Error(t, err)
		assert.Equal(t, "LocalStack returned status 404", err.Error())
		assert.NotContains(t, err.Error(), ": ")
	})

	t.Run("appends a non-empty body", func(t *testing.T) {
		t.Parallel()
		err := emulatorStatusError("pod save failed (HTTP 500)", []byte("  boom  "))
		require.Error(t, err)
		assert.Equal(t, "pod save failed (HTTP 500): boom", err.Error())
	})
}
