package azureconfig

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildEndpoint(t *testing.T) {
	t.Parallel()
	tests := []struct {
		host string
		want string
	}{
		{"localhost.localstack.cloud:4566", "https://azure.localhost.localstack.cloud:4566"},
		{"127.0.0.1:4566", "https://azure.127.0.0.1:4566"},
		{"example.com:8080", "https://azure.example.com:8080"},
	}
	for _, tc := range tests {
		t.Run(tc.host, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, BuildEndpoint(tc.host))
		})
	}
}

func TestConfigDir(t *testing.T) {
	t.Parallel()
	assert.Equal(t, filepath.Join("/home/u/.config/lstk", "azure"), ConfigDir("/home/u/.config/lstk"))
}

func TestEnv(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []string{"AZURE_CONFIG_DIR=/cfg/azure"}, Env("/cfg/azure"))
}

func TestIsHealthyOK(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/_localstack/health", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	require.NoError(t, IsHealthy(context.Background(), srv.URL))
}

func TestIsHealthyNon200(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	err := IsHealthy(context.Background(), srv.URL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "503")
}

func TestIsHealthyUnreachable(t *testing.T) {
	t.Parallel()
	err := IsHealthy(context.Background(), "http://127.0.0.1:1")
	require.Error(t, err)
}

func TestBuildCloudConfig(t *testing.T) {
	t.Parallel()
	const endpoint = "https://azure.localhost.localstack.cloud:4566"
	raw, err := BuildCloudConfig(endpoint)
	require.NoError(t, err)

	var parsed struct {
		Endpoints map[string]string `json:"endpoints"`
	}
	require.NoError(t, json.Unmarshal([]byte(raw), &parsed))

	// Every Azure endpoint must point at LocalStack — otherwise `az` would talk to real
	// Azure for that service and could hit the user's real account.
	require.NotEmpty(t, parsed.Endpoints)
	for key, value := range parsed.Endpoints {
		assert.Truef(t, strings.HasPrefix(value, endpoint),
			"endpoint %q must start with %q, got %q", key, endpoint, value)
	}

	for _, key := range []string{
		"activeDirectory",
		"activeDirectoryResourceId",
		"activeDirectoryGraphResourceId",
		"management",
		"microsoftGraphResourceId",
		"resourceManager",
		"logAnalyticsResourceId",
	} {
		_, ok := parsed.Endpoints[key]
		assert.Truef(t, ok, "cloud-config endpoints map missing key %q", key)
	}
}

func TestBuildCloudConfigTrimsTrailingSlash(t *testing.T) {
	t.Parallel()
	withSlash, err := BuildCloudConfig("https://azure.localhost.localstack.cloud:4566/")
	require.NoError(t, err)
	withoutSlash, err := BuildCloudConfig("https://azure.localhost.localstack.cloud:4566")
	require.NoError(t, err)
	assert.Equal(t, withoutSlash, withSlash, "trailing slash on input must not change the rendered cloud-config")
}

func TestIsSetUp(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.False(t, IsSetUp(dir), "fresh dir without marker should not look set up")

	require.NoError(t, os.WriteFile(filepath.Join(dir, setupMarkerFile), []byte("ok\n"), 0600))
	require.True(t, IsSetUp(dir), "marker file presence is the setup signal")
}
