package endpoint

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestResolvedSource(t *testing.T) {
	t.Run("no source present", func(t *testing.T) {
		cmd := cmdWithEndpointFlag(t)
		source, value, ok := ResolvedSource(cmd)
		assert.False(t, ok)
		assert.Empty(t, source)
		assert.Empty(t, value)
	})

	t.Run("nil cmd with no env vars set", func(t *testing.T) {
		_, _, ok := ResolvedSource(nil)
		assert.False(t, ok)
	})

	t.Run("explicit flag is reported by name, not by value alone", func(t *testing.T) {
		cmd := cmdWithEndpointFlag(t)
		require.NoError(t, cmd.Flags().Set(FlagName, "http://localhost:4566"))
		source, value, ok := ResolvedSource(cmd)
		assert.True(t, ok)
		assert.Equal(t, "--"+FlagName, source)
		assert.Equal(t, "http://localhost:4566", value)
	})

	t.Run("LSTK_ENDPOINT_URL is reported when no flag is set", func(t *testing.T) {
		t.Setenv("LSTK_ENDPOINT_URL", "http://localhost:4566")
		cmd := cmdWithEndpointFlag(t)
		source, value, ok := ResolvedSource(cmd)
		assert.True(t, ok)
		assert.Equal(t, "LSTK_ENDPOINT_URL", source)
		assert.Equal(t, "http://localhost:4566", value)
	})

	t.Run("AWS_ENDPOINT_URL is reported when neither the flag nor LSTK_ENDPOINT_URL is set", func(t *testing.T) {
		t.Setenv("AWS_ENDPOINT_URL", "http://localhost:4566")
		cmd := cmdWithEndpointFlag(t)
		source, value, ok := ResolvedSource(cmd)
		assert.True(t, ok)
		assert.Equal(t, "AWS_ENDPOINT_URL", source)
		assert.Equal(t, "http://localhost:4566", value)
	})

	t.Run("flag takes precedence over both env vars in the reported source", func(t *testing.T) {
		t.Setenv("LSTK_ENDPOINT_URL", "http://should-not-be-reported:4566")
		t.Setenv("AWS_ENDPOINT_URL", "http://should-not-be-reported-either:4566")
		cmd := cmdWithEndpointFlag(t)
		require.NoError(t, cmd.Flags().Set(FlagName, "http://localhost:4566"))
		source, _, ok := ResolvedSource(cmd)
		assert.True(t, ok)
		assert.Equal(t, "--"+FlagName, source)
	})

	t.Run("LSTK_ENDPOINT_URL takes precedence over AWS_ENDPOINT_URL in the reported source", func(t *testing.T) {
		t.Setenv("LSTK_ENDPOINT_URL", "http://localhost:4566")
		t.Setenv("AWS_ENDPOINT_URL", "http://should-not-be-reported:4566")
		cmd := cmdWithEndpointFlag(t)
		source, _, ok := ResolvedSource(cmd)
		assert.True(t, ok)
		assert.Equal(t, "LSTK_ENDPOINT_URL", source)
	})

	t.Run("no ok is reported for an unrelated flag not set", func(t *testing.T) {
		// A flag that exists but wasn't Changed must not be mistaken for a
		// present source, mirroring rawURL's existing .Changed guard.
		cmd := cmdWithEndpointFlag(t)
		source, _, ok := ResolvedSource(cmd)
		assert.False(t, ok)
		assert.Empty(t, source)
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
		{"valid https url", "https://my-instance.ephemeral-instances.localstack.cloud", false},
		{"valid https url with trailing slash trimmed", "https://localhost:4566/", false},
		{"missing scheme", "localhost:4566", true},
		{"missing host", "http://", true},
		{"not a url at all", "not a url", true},
		{"ftp scheme is rejected", "ftp://localhost:4566", true},
		{"ws scheme is rejected", "ws://localhost:4566", true},
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

func TestValidateURL_PreservesScheme(t *testing.T) {
	normalized, err := validateURL("https://localhost:4566/")
	require.NoError(t, err)
	assert.Equal(t, "https://localhost:4566", normalized)
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

func TestSwapScheme(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{"http gains a scheme's worth of TLS", "http://ls-abc.sandbox.localstack.cloud", "https://ls-abc.sandbox.localstack.cloud", true},
		{"https drops to http", "https://localhost:4566", "http://localhost:4566", true},
		{"explicit port is preserved", "http://localhost:4566", "https://localhost:4566", true},
		{"other schemes are not swapped", "ftp://localhost:4566", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := swapScheme(tt.in)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestProbeType_SchemeMismatch(t *testing.T) {
	t.Run("https URL against a plain http emulator suggests the http URL", func(t *testing.T) {
		srv := healthServer(t, `{"version":"3.0.2","services":{"s3":"available"}}`)
		defer srv.Close()

		httpsURL := strings.Replace(srv.URL, "http://", "https://", 1)
		_, err := probeType(context.Background(), httpsURL)
		require.Error(t, err)

		var mismatch *SchemeMismatchError
		require.ErrorAs(t, err, &mismatch)
		assert.Equal(t, srv.URL, mismatch.AltURL)
		assert.Contains(t, err.Error(), "could not reach LocalStack emulator at "+httpsURL)
		assert.Contains(t, err.Error(), "but "+srv.URL+" responded — retry with that URL")

		// Callers matching the plain unreachable error still match.
		var unreachableErr *UnreachableError
		assert.ErrorAs(t, err, &unreachableErr)
	})

	t.Run("no suggestion when the other scheme is equally unreachable", func(t *testing.T) {
		_, err := probeType(context.Background(), "http://127.0.0.1:1")
		require.Error(t, err)

		var mismatch *SchemeMismatchError
		assert.NotErrorAs(t, err, &mismatch)
		var unreachableErr *UnreachableError
		assert.ErrorAs(t, err, &unreachableErr)
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
