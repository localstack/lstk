package integration_test

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStatusCommandFailsWhenNotRunning(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	stdout, _, err := runLstk(t, testContext(t), "", nil, "status")
	require.Error(t, err, "expected lstk status to fail when emulator not running")
	requireExitCode(t, 1, err)
	assert.Contains(t, stdout, "is not running")
	assert.Contains(t, stdout, "Start LocalStack:")
	assert.Contains(t, stdout, "See help:")
}

func TestStatusCommandShowsResourcesWhenRunning(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)

	// Mock the LocalStack HTTP API so we can test resource display without a real LocalStack instance.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/_localstack/health":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintln(w, `{"version": "4.14.1", "services": {}}`)
		case "/_localstack/resources":
			w.Header().Set("Content-Type", "application/x-ndjson")
			_, _ = fmt.Fprintln(w, `{"AWS::S3::Bucket": [{"region_name": "global", "account_id": "000000000000", "id": "my-bucket"}]}`)
			_, _ = fmt.Fprintln(w, `{"AWS::Lambda::Function": [{"region_name": "us-east-1", "account_id": "000000000000", "id": "my-function"}]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	host := strings.TrimPrefix(server.URL, "http://")

	stdout, stderr, err := runLstk(t, ctx, "", env.With(env.LocalStackHost, host), "status")
	require.NoError(t, err, "lstk status failed: %s", stderr)
	requireExitCode(t, 0, err)
	assert.Contains(t, stdout, "running")
	assert.Contains(t, stdout, "4.14.1")
	assert.Contains(t, stdout, "2 resources")
	assert.Contains(t, stdout, "2 services")
	assert.Contains(t, stdout, "SERVICE")
	assert.Contains(t, stdout, "RESOURCE")
	assert.Contains(t, stdout, "S3")
	assert.Contains(t, stdout, "my-bucket")
	assert.Contains(t, stdout, "Lambda")
	assert.Contains(t, stdout, "my-function")
}

func TestStatusCommandWorksWithNonDefaultPort(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)

	// The mock server is assigned a random free port (guaranteed not to conflict).
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/_localstack/health":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintln(w, `{"version": "4.14.1", "services": {}}`)
		case "/_localstack/resources":
			w.Header().Set("Content-Type", "application/x-ndjson")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// Extract the port so we can bind it to the container.
	_, mockPort, err := net.SplitHostPort(server.Listener.Addr().String())
	require.NoError(t, err)

	// Simulates starting LocalStack on a non-default host port.
	startTestContainer(t, ctx, mockPort)

	// Write a config with the default port 4566
	// Simulates the user changing the config port after starting the container
	configContent := "[[containers]]\ntype = \"aws\"\ntag = \"latest\"\nport = \"4566\"\n"
	configFile := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0644))

	stdout, stderr, err := runLstk(t, ctx, "", nil, "--config", configFile, "status")
	require.NoError(t, err, "lstk status failed: %s", stderr)
	assert.Contains(t, stdout, "4.14.1")
}

func TestStatusCommandShowsNoResourcesWhenEmpty(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/_localstack/health":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintln(w, `{"version": "4.14.1", "services": {}}`)
		case "/_localstack/resources":
			w.Header().Set("Content-Type", "application/x-ndjson")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	host := strings.TrimPrefix(server.URL, "http://")

	stdout, stderr, err := runLstk(t, ctx, "", env.With(env.LocalStackHost, host), "status")
	require.NoError(t, err, "lstk status failed: %s", stderr)
	requireExitCode(t, 0, err)
	assert.Contains(t, stdout, "No resources deployed")
}
