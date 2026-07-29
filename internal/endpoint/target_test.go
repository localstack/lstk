package endpoint

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/localstack/lstk/internal/config"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func cmdWithEndpointFlag(t *testing.T) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String(FlagName, "", "")
	return cmd
}

func TestResolve_Precedence(t *testing.T) {
	srv := healthServer(t, `{"version":"3.0.2","services":{"s3":"available"}}`)
	defer srv.Close()

	t.Run("no source set falls back to Docker discovery", func(t *testing.T) {
		cmd := cmdWithEndpointFlag(t)
		target, err := Resolve(context.Background(), cmd)
		require.NoError(t, err)
		assert.Nil(t, target)
	})

	t.Run("LSTK_ENDPOINT_URL is honored", func(t *testing.T) {
		t.Setenv("LSTK_ENDPOINT_URL", srv.URL)
		cmd := cmdWithEndpointFlag(t)
		target, err := Resolve(context.Background(), cmd)
		require.NoError(t, err)
		require.NotNil(t, target)
		assert.Equal(t, srv.URL, target.URL)
		assert.Equal(t, config.EmulatorAWS, target.Type)
	})

	t.Run("AWS_ENDPOINT_URL is honored when LSTK_ENDPOINT_URL is not set", func(t *testing.T) {
		t.Setenv("AWS_ENDPOINT_URL", srv.URL)
		cmd := cmdWithEndpointFlag(t)
		target, err := Resolve(context.Background(), cmd)
		require.NoError(t, err)
		require.NotNil(t, target)
		assert.Equal(t, srv.URL, target.URL)
	})

	t.Run("LSTK_ENDPOINT_URL takes precedence over AWS_ENDPOINT_URL", func(t *testing.T) {
		other := healthServer(t, `{"version":"3.0.2","services":{"s3":"available"}}`)
		defer other.Close()
		t.Setenv("LSTK_ENDPOINT_URL", srv.URL)
		t.Setenv("AWS_ENDPOINT_URL", other.URL)
		cmd := cmdWithEndpointFlag(t)
		target, err := Resolve(context.Background(), cmd)
		require.NoError(t, err)
		require.NotNil(t, target)
		assert.Equal(t, srv.URL, target.URL)
	})

	t.Run("flag takes precedence over both env vars", func(t *testing.T) {
		t.Setenv("LSTK_ENDPOINT_URL", "http://should-not-be-used:4566")
		t.Setenv("AWS_ENDPOINT_URL", "http://should-not-be-used-either:4566")
		cmd := cmdWithEndpointFlag(t)
		require.NoError(t, cmd.Flags().Set(FlagName, srv.URL))
		target, err := Resolve(context.Background(), cmd)
		require.NoError(t, err)
		require.NotNil(t, target)
		assert.Equal(t, srv.URL, target.URL)
	})

	t.Run("AWS_ENDPOINT_URL applies uniformly, not just to AWS-specific commands", func(t *testing.T) {
		// Resolve has no notion of "which command" — the caller (cmd/) decides
		// what to do with the detected type. Here we just confirm the env var
		// itself is consulted regardless of any such restriction.
		t.Setenv("AWS_ENDPOINT_URL", srv.URL)
		cmd := cmdWithEndpointFlag(t)
		target, err := Resolve(context.Background(), cmd)
		require.NoError(t, err)
		require.NotNil(t, target)
	})
}

func TestValidateURL(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{"valid http url", "http://localhost:4566", false},
		{"valid http url with trailing slash trimmed", "http://localhost:4566/", false},
		{"missing scheme", "localhost:4566", true},
		{"missing host", "http://", true},
		{"not a url at all", "not a url", true},
		{"https is rejected", "https://localhost:4566", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validateURL(tt.raw)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestResolve_MalformedURLFailsFast(t *testing.T) {
	cmd := cmdWithEndpointFlag(t)
	require.NoError(t, cmd.Flags().Set(FlagName, "not-a-url"))
	target, err := Resolve(context.Background(), cmd)
	assert.Nil(t, target)
	assert.Error(t, err)
}

func TestProbeType(t *testing.T) {
	t.Run("aws detected via services map", func(t *testing.T) {
		srv := healthServer(t, `{"version":"3.0.2","services":{"s3":"available","sqs":"available"}}`)
		defer srv.Close()
		typ, err := probeType(context.Background(), srv.URL)
		require.NoError(t, err)
		assert.Equal(t, config.EmulatorAWS, typ)
	})

	t.Run("snowflake detected via services map", func(t *testing.T) {
		srv := healthServer(t, `{"version":"3.0.2","services":{"snowflake":"available"}}`)
		defer srv.Close()
		typ, err := probeType(context.Background(), srv.URL)
		require.NoError(t, err)
		assert.Equal(t, config.EmulatorSnowflake, typ)
	})

	t.Run("azure detected via info fallback when health lacks version", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/_localstack/health", func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"services":{}}`))
		})
		mux.HandleFunc("/_localstack/info", func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"version":"3.0.2"}`))
		})
		srv := httptest.NewServer(mux)
		defer srv.Close()

		typ, err := probeType(context.Background(), srv.URL)
		require.NoError(t, err)
		assert.Equal(t, config.EmulatorAzure, typ)
	})

	t.Run("indeterminate when services map has no recognizable signature", func(t *testing.T) {
		srv := healthServer(t, `{"version":"3.0.2","services":{"something-unknown":"available"}}`)
		defer srv.Close()
		_, err := probeType(context.Background(), srv.URL)
		require.Error(t, err)
		var indeterminate *IndeterminateTypeError
		assert.ErrorAs(t, err, &indeterminate)
	})

	t.Run("indeterminate when neither health nor info has a version", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/_localstack/health", func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"services":{}}`))
		})
		mux.HandleFunc("/_localstack/info", func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{}`))
		})
		srv := httptest.NewServer(mux)
		defer srv.Close()

		_, err := probeType(context.Background(), srv.URL)
		require.Error(t, err)
		var indeterminate *IndeterminateTypeError
		assert.ErrorAs(t, err, &indeterminate)
	})

	t.Run("unreachable endpoint fails closed", func(t *testing.T) {
		_, err := probeType(context.Background(), "http://127.0.0.1:1")
		require.Error(t, err)
		var unreachable *UnreachableError
		assert.ErrorAs(t, err, &unreachable)
	})

	t.Run("non-LocalStack service (bad status code) fails closed", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()
		_, err := probeType(context.Background(), srv.URL)
		require.Error(t, err)
		var unreachable *UnreachableError
		assert.ErrorAs(t, err, &unreachable)
	})

	t.Run("non-JSON response fails closed", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("<html>not localstack</html>"))
		}))
		defer srv.Close()
		_, err := probeType(context.Background(), srv.URL)
		require.Error(t, err)
		var unreachable *UnreachableError
		assert.ErrorAs(t, err, &unreachable)
	})
}

func TestTarget_HostPort(t *testing.T) {
	target := &Target{URL: "http://localhost:4566"}
	assert.Equal(t, "localhost:4566", target.HostPort())
}

// healthServer starts an httptest.Server that always serves body at
// /_localstack/health with a 200 status.
func healthServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_localstack/health" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
}
