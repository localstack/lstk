package integration_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// returns a mock server for catalog and license endpoints.
// Empty catalogVersion → 503. The returned *string captures the product version from license requests.
func createVersionResolutionMockServer(t *testing.T, catalogVersion string, licenseSuccess bool) (*httptest.Server, *string) {
	t.Helper()
	capturedVersion := new(string)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/license/catalog/"):
			if catalogVersion == "" {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
			emulatorType := parts[len(parts)-2]
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"emulator_type":%q,"version":%q}`, emulatorType, catalogVersion)
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
				w.WriteHeader(http.StatusOK)
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

// Verifies that when the catalog API returns a version, the license request uses
// that version (not "latest"), allowing license validation to happen before pulling the image.
func TestVersionResolvedViaCatalog(t *testing.T) {
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)

	cleanup()
	t.Cleanup(cleanup)

	mockServer, capturedVersion := createVersionResolutionMockServer(t, "4.14.0", true)

	ctx := testContext(t)
	stdout, stderr, err := runLstk(t, ctx, "", env.With(env.APIEndpoint, mockServer.URL), "start")
	require.NoError(t, err, "lstk start failed:\nstdout: %s\nstderr: %s", stdout, stderr)

	assert.Equal(t, "4.14.0", *capturedVersion,
		"license request should carry the version returned by the catalog API")
	assert.NotEqual(t, "latest", *capturedVersion,
		"license request should not use the unresolved 'latest' tag")
}

// Verifies that when the catalog endpoint is unavailable, the version is resolved
// by inspecting the pulled image and used for licensing.
func TestVersionFallsBackToImageInspectionWhenCatalogFails(t *testing.T) {
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)

	cleanup()
	t.Cleanup(cleanup)

	// Catalog returns 503; license accepts all requests
	mockServer, capturedVersion := createVersionResolutionMockServer(t, "", true)

	ctx := testContext(t)
	stdout, stderr, err := runLstk(t, ctx, "", env.With(env.APIEndpoint, mockServer.URL), "start")
	require.NoError(t, err, "lstk start should succeed via image inspection fallback:\nstdout: %s\nstderr: %s", stdout, stderr)

	assert.NotEmpty(t, *capturedVersion, "license request should carry a version resolved from image inspection")
	assert.NotEqual(t, "latest", *capturedVersion, "resolved version should not be the unresolved 'latest' tag")
}

// Verifies that when both the catalog endpoint is unavailable and the license
// validation fails in the image inspection fallback path, the command exits with a clear error.
func TestCommandFailsNicelyWhenCatalogAndLicenseBothFail(t *testing.T) {
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)

	cleanup()
	t.Cleanup(cleanup)

	// Catalog unavailable; license rejects all requests
	mockServer, _ := createVersionResolutionMockServer(t, "", false)

	ctx := testContext(t)
	stdout, stderr, err := runLstk(t, ctx, "",
		env.With(env.APIEndpoint, mockServer.URL), "start")
	require.Error(t, err, "expected lstk start to fail when catalog is down and license check fails")
	assert.Contains(t, stderr, "license validation failed",
		"stdout: %s", stdout)
}
