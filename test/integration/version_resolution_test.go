package integration_test

import (
	"encoding/json"
	"io"
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

// returns a mock license server that captures the product version from license requests.
// licenseSuccess controls whether the server returns 200 or 403.
func createLicenseMockServer(t *testing.T, licenseSuccess bool) (*httptest.Server, *string) {
	t.Helper()
	capturedVersion := new(string)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/license/request":
			body, _ := io.ReadAll(r.Body)
			var req struct {
				Product struct {
					Version string `json:"version"`
				} `json:"product"`
			}
			_ = json.Unmarshal(body, &req)
			*capturedVersion = req.Product.Version
			if licenseSuccess {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"license_type":"pro"}`))
			} else {
				w.WriteHeader(http.StatusForbidden)
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, capturedVersion
}

// Verifies that when "latest" is configured, the version is resolved by inspecting
// the pulled image and the license request carries that resolved version (not "latest").
func TestVersionResolvedViaImageInspection(t *testing.T) {
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)

	cleanup()
	t.Cleanup(cleanup)

	mockServer, capturedVersion := createLicenseMockServer(t, true)

	ctx := testContext(t)
	stdout, stderr, err := runLstk(t, ctx, "", env.With(env.APIEndpoint, mockServer.URL), "start")
	require.NoError(t, err, "lstk start failed:\nstdout: %s\nstderr: %s", stdout, stderr)

	assert.NotEmpty(t, *capturedVersion, "license request should carry a version resolved from image inspection")
	assert.NotEqual(t, "latest", *capturedVersion, "resolved version should not be the unresolved 'latest' tag")
	assert.Contains(t, stdout, "Checking license")

	semverLike := strings.Contains(*capturedVersion, ".") || strings.Contains(*capturedVersion, "-")
	assert.True(t, semverLike, "resolved version %q should look like a real version", *capturedVersion)
}

// Verifies that when the license check fails after image inspection, the command
// exits with a clear error message.
func TestCommandFailsNicelyWhenLicenseCheckFails(t *testing.T) {
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)

	cleanup()
	t.Cleanup(cleanup)

	mockServer, _ := createLicenseMockServer(t, false)

	ctx := testContext(t)
	stdout, stderr, err := runLstk(t, ctx, "",
		env.With(env.APIEndpoint, mockServer.URL), "start")
	require.Error(t, err, "expected lstk start to fail when license check fails")
	assert.Contains(t, stderr, "license validation failed",
		"stdout: %s", stdout)
}

// Verifies that pinned tags are validated before pulling (fail-fast path).
func TestPinnedTagValidatedPrePull(t *testing.T) {
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)

	cleanup()
	t.Cleanup(cleanup)

	// License rejects all requests — with a pinned tag, failure should happen before any pull
	mockServer, capturedVersion := createLicenseMockServer(t, false)

	configFile := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(configFile, []byte(`
[[containers]]
type = "aws"
tag  = "4.0.0"
port = "4566"
`), 0644))

	ctx := testContext(t)
	stdout, stderr, err := runLstk(t, ctx, "",
		env.With(env.APIEndpoint, mockServer.URL), "--config", configFile, "start")
	require.Error(t, err, "expected lstk start to fail when license check fails")
	assert.Contains(t, stderr, "license validation failed",
		"stdout: %s", stdout)
	assert.Equal(t, "4.0.0", *capturedVersion, "pinned tag should be sent directly to the license API without image inspection")
}
