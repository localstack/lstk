package azureconfig

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildEndpoint(t *testing.T) {
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
			assert.Equal(t, tc.want, BuildEndpoint(tc.host))
		})
	}
}

func TestBuildCloudConfig(t *testing.T) {
	raw, err := buildCloudConfig("https://azure.localhost.localstack.cloud:4566")
	require.NoError(t, err)

	var got struct {
		Endpoints map[string]string `json:"endpoints"`
	}
	require.NoError(t, json.Unmarshal(raw, &got))

	const bare = "https://azure.localhost.localstack.cloud:4566"
	const slash = bare + "/"

	// Endpoints that the `az` CLI appends paths to must end with "/"
	// (mirrors the azlocal reference implementation).
	assert.Equal(t, slash, got.Endpoints["management"])
	assert.Equal(t, slash, got.Endpoints["microsoftGraphResourceId"])
	assert.Equal(t, slash, got.Endpoints["resourceManager"])

	// AD/log endpoints are used as resource identifiers and must NOT end with "/".
	assert.Equal(t, bare, got.Endpoints["activeDirectory"])
	assert.Equal(t, bare, got.Endpoints["activeDirectoryResourceId"])
	assert.Equal(t, bare, got.Endpoints["activeDirectoryGraphResourceId"])
	assert.Equal(t, bare, got.Endpoints["logAnalyticsResourceId"])
}

func TestBuildCloudConfigStripsExistingTrailingSlash(t *testing.T) {
	raw, err := buildCloudConfig("https://azure.localhost.localstack.cloud:4566/")
	require.NoError(t, err)

	var got struct {
		Endpoints map[string]string `json:"endpoints"`
	}
	require.NoError(t, json.Unmarshal(raw, &got))

	// No double-slash even if caller passes a trailing slash.
	assert.Equal(t, "https://azure.localhost.localstack.cloud:4566/", got.Endpoints["management"])
	assert.Equal(t, "https://azure.localhost.localstack.cloud:4566", got.Endpoints["activeDirectory"])
}

func TestIsRunningOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/_localstack/health", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	require.NoError(t, IsRunning(context.Background(), srv.URL))
}

func TestIsRunningNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	err := IsRunning(context.Background(), srv.URL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "503")
}

func TestIsRunningUnreachable(t *testing.T) {
	// Closed port on loopback should refuse the connection quickly.
	err := IsRunning(context.Background(), "http://127.0.0.1:1")
	require.Error(t, err)
}
