package integration_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bundledLicenseImage is a local-only stand-in for a LocalStack Enterprise
// (offline) image: it carries LOCALSTACK_AUTH_TOKEN_OVERRIDE like the real one.
// Its "latest" tag is deliberate: a floating tag would normally be pulled.
const bundledLicenseImage = "lstk-bundled-license-test:latest"

// commitBundledLicenseImage builds bundledLicenseImage. Instead of an emulator
// it prints what it received from lstk and exits, so the start fails with that
// output as "Last container output", which is what the tests assert on.
func commitBundledLicenseImage(t *testing.T, ctx context.Context) {
	t.Helper()

	reader, err := dockerClient.ImagePull(ctx, testImage, client.ImagePullOptions{})
	require.NoError(t, err, "failed to pull test image")
	_, _ = io.Copy(io.Discard, reader)
	_ = reader.Close()

	const srcName = "lstk-bundled-license-src"
	_, _ = dockerClient.ContainerRemove(ctx, srcName, client.ContainerRemoveOptions{Force: true})
	resp, err := dockerClient.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{Image: testImage},
		Name:   srcName,
	})
	require.NoError(t, err, "failed to create source container")
	t.Cleanup(func() {
		_, _ = dockerClient.ContainerRemove(context.Background(), resp.ID, client.ContainerRemoveOptions{Force: true})
	})

	_, err = dockerClient.ContainerCommit(ctx, resp.ID, client.ContainerCommitOptions{
		Reference: bundledLicenseImage,
		Changes: []string{
			"ENV LOCALSTACK_AUTH_TOKEN_OVERRIDE=bundled-test-token",
			"ENV LOCALSTACK_BUILD_VERSION=2026.8.4",
			`CMD ["sh", "-c", "echo auth-token=[${LOCALSTACK_AUTH_TOKEN-unset}]; if [ -e /etc/localstack/conf.d/license.json ]; then echo cached-license=mounted; else echo cached-license=absent; fi"]`,
		},
	})
	require.NoError(t, err, "failed to commit bundled-license image")
	t.Cleanup(func() {
		_, _ = dockerClient.ImageRemove(context.Background(), bundledLicenseImage, client.ImageRemoveOptions{Force: true})
	})
}

// bundledLicenseEnv returns an isolated environment without any auth token, a
// cached license.json for the user's own token (darwin/linux), and a license
// server that counts every request it receives.
func bundledLicenseEnv(t *testing.T) (env.Environ, string, *int32) {
	t.Helper()

	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)

	home := t.TempDir()
	scheduleVolumeCleanup(t, home)
	cacheHome := filepath.Join(home, ".cache")
	var licenseDir string
	switch runtime.GOOS {
	case "darwin":
		licenseDir = filepath.Join(home, "Library", "Caches", "lstk")
	case "linux":
		licenseDir = filepath.Join(cacheHome, "lstk")
	}
	if licenseDir != "" {
		require.NoError(t, os.MkdirAll(licenseDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(licenseDir, "license.json"), []byte(`{"license_type":"cached"}`), 0600))
	}

	configFile := filepath.Join(home, "config.toml")
	require.NoError(t, os.WriteFile(configFile, []byte("[[containers]]\ntype = \"aws\"\nport = \"4566\"\n"), 0644))

	e := env.Environ(testEnvWithHome(home, "")).
		Without(env.AuthToken).
		With(env.APIEndpoint, srv.URL).
		With(env.DisableEvents, "1").
		With("XDG_CACHE_HOME", cacheHome)
	return e, configFile, &hits
}

// An offline image brings its own license, so starting it must need nothing
// from the network or the user: no token, no login, no pull, no license check,
// and no blank LOCALSTACK_AUTH_TOKEN or foreign cached license put into it.
func TestStartWithBundledLicenseImageNeedsNoTokenPullOrLicenseCheck(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	commitBundledLicenseImage(t, ctx)
	e, configFile, licenseHits := bundledLicenseEnv(t)

	stdout, stderr, _ := runLstk(t, ctx, "", e, "--config", configFile, "--non-interactive", "start", "--image", bundledLicenseImage)
	combined := stdout + stderr

	assert.NotContains(t, combined, "authentication required", "a bundled-license image must not require an auth token")
	assert.Contains(t, combined, "Using local image "+bundledLicenseImage, "the local offline image must be used as is")
	assert.NotContains(t, combined, "Pulling", "a bundled-license image must not be pulled, even on a floating tag")
	assert.NotContains(t, combined, "Checking license", "no license pre-flight for a bundled-license image")
	assert.Equal(t, int32(0), atomic.LoadInt32(licenseHits), "the license server must not be contacted")
	assert.Contains(t, combined, "auth-token=[unset]", "LOCALSTACK_AUTH_TOKEN must be absent, not blank")
	if runtime.GOOS != "windows" {
		assert.Contains(t, combined, "cached-license=absent", "the user's cached license must not be mounted next to the bundled one")
	}
}

// The interactive start must not fall into the device login either: that is
// what blocked offline users, since the login needs the platform API.
func TestStartInteractiveWithBundledLicenseImageSkipsLogin(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	commitBundledLicenseImage(t, ctx)
	e, configFile, licenseHits := bundledLicenseEnv(t)

	out, _ := runLstkInPTY(t, ctx, e, "--config", configFile, "start", "--image", bundledLicenseImage)

	assert.NotContains(t, out, "Waiting for authorization", "no device login for a bundled-license image")
	assert.Contains(t, out, "██▙█▙█", "the header must still be shown when the login is skipped")
	assert.NotContains(t, out, "No license", "a bundled license must not be labelled as missing")
	assert.Contains(t, out, "Using local image "+bundledLicenseImage)
	assert.Equal(t, int32(0), atomic.LoadInt32(licenseHits), "neither the login nor the license check may contact the platform")
	assert.Contains(t, out, "auth-token=[unset]")
}
