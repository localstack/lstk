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

// returns a mock license server that answers license requests with the given
// status and body, capturing the product version from each request.
func createLicenseMockServer(t *testing.T, status int, body string) (*httptest.Server, *string) {
	t.Helper()
	capturedVersion := new(string)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/license/request":
			reqBody, _ := io.ReadAll(r.Body)
			var req struct {
				Product struct {
					Version string `json:"version"`
				} `json:"product"`
			}
			_ = json.Unmarshal(reqBody, &req)
			*capturedVersion = req.Product.Version
			if body != "" {
				w.Header().Set("Content-Type", "application/json")
			}
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
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

	mockServer, capturedVersion := createLicenseMockServer(t, http.StatusOK, `{"license_type":"pro"}`)

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

	mockServer, _ := createLicenseMockServer(t, http.StatusForbidden, "")

	ctx := testContext(t)
	stdout, stderr, err := runLstk(t, ctx, "",
		env.With(env.APIEndpoint, mockServer.URL), "start")
	require.Error(t, err, "expected lstk start to fail when license check fails")
	assert.Contains(t, stderr, "license validation failed",
		"stdout: %s", stdout)
}

// Verifies that a tag the license server cannot parse as a version (e.g. "dev"
// nightlies or custom enterprise tags) does not block the start: the pre-flight is
// skipped with a warning and license validation is left to the emulator itself
// (DEVX-912).
func TestUnsupportedTagSkipsPreflightValidation(t *testing.T) {
	requireDocker(t)
	t.Parallel()

	mockServer, capturedVersion := createLicenseMockServer(t, http.StatusBadRequest,
		`{"error": true, "message": "licensing.license.format:illegal version string dev"}`)

	// The image override points at an unreachable local registry so the run stops
	// at the pull right after the license gate (the real dev image would be a
	// multi-GB download). The pull failure is this test's expected exit path — the
	// behavior under test is that the run gets *past* license validation.
	configFile := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(configFile, []byte(`
[[containers]]
type  = "aws"
tag   = "dev"
port  = "4599"
image = "127.0.0.1:1/localstack-pro"
`), 0644))

	ctx := testContext(t)
	environ := env.Environ(testEnvWithHome(t.TempDir(), "")).
		With(env.APIEndpoint, mockServer.URL).
		With(env.AuthToken, "test-token-for-unsupported-tag")
	stdout, stderr, err := runLstk(t, ctx, "", environ, "--config", configFile, "start")

	out := stdout + "\n" + stderr
	assert.Equal(t, "dev", *capturedVersion, "the configured tag should be sent to the license API")
	assert.NotContains(t, out, "license validation failed",
		"an unparseable tag must not be treated as a license rejection")
	assert.Contains(t, out, `does not support tag "dev"`,
		"the skipped pre-flight should be surfaced as a warning")
	require.Error(t, err, "the start should proceed to the pull and fail on the unreachable registry")
	requireExitCode(t, 1, err)
	assert.Contains(t, out, "Failed to pull")
}

// Verifies that pinned tags are validated before pulling (fail-fast path).
func TestPinnedTagValidatedPrePull(t *testing.T) {
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)

	cleanup()
	t.Cleanup(cleanup)

	// License rejects all requests — with a pinned tag, failure should happen before any pull
	mockServer, capturedVersion := createLicenseMockServer(t, http.StatusForbidden, "")

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
