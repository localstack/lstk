package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/creack/pty"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLicenseValidationSuccess(t *testing.T) {
	requireDocker(t)
	authToken := env.Require(t, env.AuthToken)

	cleanupLicense()
	t.Cleanup(cleanupLicense)

	validationErrors := make(chan error, 1)

	// Mock platform API that returns success
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/license/request" && r.Method == http.MethodPost {
			var req map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "invalid request", http.StatusBadRequest)
				return
			}

			// Validate with safe type assertions
			product, ok := req["product"].(map[string]interface{})
			if !ok || product["name"] != "localstack-pro" {
				validationErrors <- fmt.Errorf("invalid product field")
				http.Error(w, "invalid product", http.StatusBadRequest)
				return
			}

			credentials, ok := req["credentials"].(map[string]interface{})
			if !ok || credentials["token"] != authToken {
				validationErrors <- fmt.Errorf("invalid credentials field")
				http.Error(w, "invalid credentials", http.StatusBadRequest)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"license_type":"pro"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer mockServer.Close()

	ctx := testContext(t)
	_, stderr, err := runLstk(t, ctx, "", env.With(env.APIEndpoint, mockServer.URL), "start")

	// Check for validation errors from handler
	select {
	case validationErr := <-validationErrors:
		t.Fatalf("request validation failed: %v", validationErr)
	default:
	}

	require.NoError(t, err, "lstk start failed: %s", stderr)
	requireExitCode(t, 0, err)

	inspect, err := dockerClient.ContainerInspect(ctx, containerName, client.ContainerInspectOptions{})
	require.NoError(t, err, "failed to inspect container")
	assert.True(t, inspect.Container.State.Running, "container should be running")
}

func TestLicenseValidationFailure(t *testing.T) {
	requireDocker(t)
	cleanupLicense()
	t.Cleanup(cleanupLicense)

	mockServer := createMockLicenseServer(false)
	defer mockServer.Close()

	ctx := testContext(t)
	stdout, stderr, err := runLstk(t, ctx, "", env.With(env.APIEndpoint, mockServer.URL).With(env.AuthToken, "test-token-for-license-validation"), "start")
	require.Error(t, err, "expected lstk start to fail with forbidden license")
	requireExitCode(t, 1, err)
	assert.Contains(t, stdout, "License validation failed")
	assert.Contains(t, stdout, "invalid, inactive, or expired")
	assert.Contains(t, stdout, "lstk logout", "the error should point at re-authentication")
	assert.NotContains(t, stderr, "license validation failed", "the error event replaces the raw stderr error")

	_, err = dockerClient.ContainerInspect(ctx, containerName, client.ContainerInspectOptions{})
	assert.Error(t, err, "container should not exist after license failure")
}

// TestLicenseRejectionOffersReloginAndRetries covers DEVX-658: a definitively
// rejected token (e.g. one that predates a license purchase) must offer a fresh
// login in interactive mode and retry the start with the new token, instead of
// requiring a manual `lstk logout`.
func TestLicenseRejectionOffersReloginAndRetries(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PTY not supported on Windows")
	}
	requireDocker(t)
	realToken := env.Require(t, env.AuthToken)

	cleanup()
	t.Cleanup(cleanup)
	cleanupLicense()
	t.Cleanup(cleanupLicense)

	staleToken := "stale-token-predating-license-purchase"
	authReqID := "relogin-auth-req-id"

	var staleRejected atomic.Bool
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/auth/request":
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"id": authReqID, "code": "TEST123", "exchange_token": "relogin-exchange-token",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/auth/request/"+authReqID:
			_ = json.NewEncoder(w).Encode(map[string]bool{"confirmed": true})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/auth/request/"+authReqID+"/exchange":
			_ = json.NewEncoder(w).Encode(map[string]string{"id": authReqID, "auth_token": "Bearer test-bearer"})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/license/credentials":
			_ = json.NewEncoder(w).Encode(map[string]string{"token": realToken})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/license/request":
			var req struct {
				Credentials struct {
					Token string `json:"token"`
				} `json:"credentials"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.Credentials.Token != realToken {
				staleRejected.Store(true)
				w.WriteHeader(http.StatusForbidden)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"license_type":"enterprise"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockServer.Close()

	// A generous budget: this flow pulls the image, runs a full login round-trip,
	// and boots the emulator — the default 2-minute test context is too tight on
	// CI runners. LSTK_STARTUP_TIMEOUT keeps the interactive "keep waiting?"
	// prompt (20s default) from pausing the start while the emulator boots,
	// since this test only answers the re-login and login prompts.
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	t.Cleanup(cancel)
	environ, _ := fakeBrowserOpener(t, env.With(env.AuthToken, staleToken).With(env.APIEndpoint, mockServer.URL).With(env.WebAppURL, mockServer.URL))
	environ = append(environ, "LSTK_STARTUP_TIMEOUT=5m")

	// An explicit config prevents firstRun=true, which would block the TUI on the
	// emulator selection prompt before the license check runs.
	configFile := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(configFile, []byte("[[containers]]\ntype = \"aws\"\ntag = \"latest\"\nport = \"4566\"\n"), 0644))

	cmd := exec.CommandContext(ctx, binaryPath(), "start", "--config", configFile)
	cmd.Env = environ
	ptmx, err := pty.Start(cmd)
	require.NoError(t, err, "failed to start command in PTY")
	defer func() { _ = ptmx.Close() }()

	out := &syncBuffer{}
	outputCh := make(chan struct{})
	go func() {
		_, _ = io.Copy(out, ptmx)
		close(outputCh)
	}()

	// The stale token is rejected; the re-login prompt appears. Press ENTER.
	// The wait covers a cold image pull on CI runners.
	require.Eventually(t, func() bool {
		return bytes.Contains(out.Bytes(), []byte("Log in again"))
	}, 3*time.Minute, 100*time.Millisecond, "the re-login prompt should appear after the license rejection")
	_, err = ptmx.Write([]byte("\r"))
	require.NoError(t, err)

	// The login flow runs; confirm it once the completion prompt appears.
	require.Eventually(t, func() bool {
		return bytes.Contains(out.Bytes(), []byte("key when complete"))
	}, 30*time.Second, 100*time.Millisecond, "the login completion prompt should appear")
	_, err = ptmx.Write([]byte("\r"))
	require.NoError(t, err)

	// After a successful start, the post-start AWS profile setup asks a Y/n
	// question when the runner's ~/.aws has no matching profile. It is
	// conditional (skipped when the profile already matches), so watch for it
	// and decline instead of requiring it.
	go func() {
		var answered atomic.Bool
		for {
			select {
			case <-outputCh:
				return
			case <-time.After(200 * time.Millisecond):
				if !answered.Load() && bytes.Contains(out.Bytes(), []byte("~/.aws? [Y/n]")) {
					answered.Store(true)
					_, _ = ptmx.Write([]byte("n"))
				}
			}
		}
	}()

	err = cmd.Wait()
	<-outputCh
	if err != nil {
		// The PTY transcript cannot explain a container that never became
		// healthy — capture the emulator's own view before cleanup removes it.
		diagCtx, diagCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer diagCancel()
		if inspectOut, ierr := exec.CommandContext(diagCtx, "docker", "inspect", "--format", "{{.State.Status}} exit={{.State.ExitCode}}", containerName).CombinedOutput(); ierr == nil {
			t.Logf("container state: %s", strings.TrimSpace(string(inspectOut)))
		}
		if logsOut, lerr := exec.CommandContext(diagCtx, "docker", "logs", "--tail", "150", containerName).CombinedOutput(); lerr == nil {
			t.Logf("container logs (tail):\n%s", string(logsOut))
		}
		if resp, herr := http.Get("http://localhost:4566/_localstack/health"); herr == nil {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
			_ = resp.Body.Close()
			t.Logf("health endpoint: HTTP %d %s", resp.StatusCode, string(body))
		} else {
			t.Logf("health endpoint unreachable: %v", herr)
		}
	}
	require.NoError(t, err, "start should succeed after re-login: %s", out.String())
	assert.True(t, staleRejected.Load(), "the stale token must have been rejected by the license server first")
	assert.Contains(t, out.String(), "Valid license")

	inspect, err := dockerClient.ContainerInspect(ctx, containerName, client.ContainerInspectOptions{})
	require.NoError(t, err, "failed to inspect container")
	assert.True(t, inspect.Container.State.Running, "container should be running after the retried start")

	// lstk's file-keyring fallback stores the token next to the active config
	// file (ConfigDir follows --config), so on headless CI runners the token
	// lands in this test's temp dir — not in the default config dir the shared
	// GetAuthTokenFromKeyring helper reads.
	var storedToken string
	if useFileKeyring {
		data, rerr := os.ReadFile(filepath.Join(filepath.Dir(configFile), authTokenFile))
		require.NoError(t, rerr, "the re-logged-in token should be stored in the file keyring next to the config file")
		storedToken = string(data)
	} else {
		storedToken, err = GetAuthTokenFromKeyring()
		require.NoError(t, err, "the re-logged-in token should be stored in the system keyring")
	}
	assert.Equal(t, realToken, storedToken, "the fresh token must replace the rejected one")
}

func licenseFilePath(t *testing.T) string {
	t.Helper()
	cacheDir, err := os.UserCacheDir()
	require.NoError(t, err)
	return filepath.Join(cacheDir, "lstk", "license.json")
}

func cleanupLicense() {
	ctx := context.Background()
	_, _ = dockerClient.ContainerRemove(ctx, containerName, client.ContainerRemoveOptions{Force: true})
	if cacheDir, err := os.UserCacheDir(); err == nil {
		_ = os.Remove(filepath.Join(cacheDir, "lstk", "license.json"))
	}
}

func TestLicenseCacheAndMount(t *testing.T) {
	requireDocker(t)
	env.Require(t, env.AuthToken)

	cleanupLicense()
	t.Cleanup(cleanupLicense)

	licenseBody := `{"license":"test-license-data"}`
	mockServer := createMockLicenseServerWithBody(licenseBody)
	defer mockServer.Close()

	ctx := testContext(t)
	_, stderr, err := runLstk(t, ctx, "", env.With(env.APIEndpoint, mockServer.URL), "start")
	require.NoError(t, err, "lstk start failed: %s", stderr)

	data, err := os.ReadFile(licenseFilePath(t))
	require.NoError(t, err, "license cache file should exist after successful start")
	assert.Equal(t, licenseBody, string(data))

	inspect, err := dockerClient.ContainerInspect(ctx, containerName, client.ContainerInspectOptions{})
	require.NoError(t, err, "failed to inspect container")

	var mounted bool
	for _, m := range inspect.Container.Mounts {
		if m.Destination == "/etc/localstack/conf.d/license.json" {
			mounted = true
			break
		}
	}
	assert.True(t, mounted, "license file should be mounted into container at /etc/localstack/conf.d/license.json")
}
