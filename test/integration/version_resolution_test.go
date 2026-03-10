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

// returns a mock server for the catalog and license endpoints.
// catalogVersion non-empty → catalog returns it; empty → 503. licenseSuccess controls 200 vs 403.
// The returned string pointer receives the product version from each license request.
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
			// Path: /v1/license/catalog/{emulatorType}/version
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

// getRealCatalogVersion fetches the current latest version from the real LocalStack catalog API.
// This guarantees the returned version has a corresponding Docker Hub tag, since the catalog
// only serves released versions.  The test is skipped if the catalog is unreachable.
func getRealCatalogVersion(t *testing.T) string {
	t.Helper()
	resp, err := http.Get("https://api.localstack.cloud/v1/license/catalog/aws/version")
	if err != nil {
		t.Skipf("real catalog API not reachable, skipping: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Skipf("real catalog API returned %d, skipping", resp.StatusCode)
	}
	var v struct {
		Version string `json:"version"`
	}
	body, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(body, &v)
	if v.Version == "" {
		t.Skip("real catalog API returned empty version, skipping")
	}
	return v.Version
}

// TestStartResolvesVersionFromCatalogAPI verifies that when the catalog API returns a version,
// that version is used for the image pull and license validation (instead of image inspection).
func TestStartResolvesVersionFromCatalogAPI(t *testing.T) {
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)

	cleanup()
	t.Cleanup(cleanup)

	// Use the version from the real catalog: it is guaranteed to have a matching Docker Hub tag.
	catalogVersion := getRealCatalogVersion(t)

	mockServer, capturedVersion := createVersionResolutionMockServer(t, catalogVersion, true)

	ctx := testContext(t)
	stdout, stderr, err := runLstk(t, ctx, "", env.With(env.APIEndpoint, mockServer.URL), "start")
	require.NoError(t, err, "lstk start failed:\nstdout: %s\nstderr: %s", stdout, stderr)

	assert.Contains(t, stdout, fmt.Sprintf("localstack/localstack-pro:%s", catalogVersion),
		"should pull the version resolved by the catalog API, not :latest")
	assert.Equal(t, catalogVersion, *capturedVersion,
		"license request should carry the version returned by the catalog API")
}

// TestStartFallsBackToImageVersionWhenCatalogFails verifies that when the catalog endpoint is
// unavailable, the version is resolved by inspecting the pulled image instead.
func TestStartFallsBackToImageVersionWhenCatalogFails(t *testing.T) {
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)

	cleanup()
	t.Cleanup(cleanup)

	// Catalog returns 503; license accepts all requests and captures the resolved version.
	mockServer, capturedVersion := createVersionResolutionMockServer(t, "", true)

	ctx := testContext(t)
	stdout, stderr, err := runLstk(t, ctx, "", env.With(env.APIEndpoint, mockServer.URL), "start")
	require.NoError(t, err, "lstk start should succeed via image inspection fallback:\nstdout: %s\nstderr: %s", stdout, stderr)

	assert.NotEmpty(t, *capturedVersion, "license request should carry a version resolved from image inspection")
	assert.NotEqual(t, "latest", *capturedVersion, "resolved version should not be the unresolved 'latest' tag")
}

// TestStartFailsNicelyWhenCatalogAndLicenseBothFail verifies that when both the catalog endpoint
// and license validation fail, the command exits with a clear user-facing error.
func TestStartFailsNicelyWhenCatalogAndLicenseBothFail(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	// Catalog returns 503; license returns 403 (forbidden).
	mockServer, _ := createVersionResolutionMockServer(t, "", false)

	stdout, stderr, err := runLstk(t, testContext(t), "",
		env.With(env.AuthToken, "invalid-token").With(env.APIEndpoint, mockServer.URL), "start")
	require.Error(t, err, "expected lstk start to fail when catalog and license both fail")
	assert.Contains(t, stderr, "license validation failed",
		"stdout: %s", stdout)
}
