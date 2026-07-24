package integration_test

import (
	"archive/zip"
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// deadLocalStackHost pins LOCALSTACK_HOST to a closed port so the emulator
// reachability probe deterministically fails. Negative-path tests ("is not
// running", "Docker is not available") must set this: without it they would
// probe 127.0.0.1:4566 and attach to a real LocalStack instance on the
// developer's machine (e.g. one running from source).
const deadLocalStackHost = "127.0.0.1:1"

// mockLocalStackInfoServer serves /_localstack/info the way a running
// LocalStack instance (container or from-source) does, so tests can stand in
// for an emulator lstk did not start. Extra handlers extend the mux for
// endpoints a command calls after discovery (reset, snapshot, ...).
func mockLocalStackInfoServer(t *testing.T, extra map[string]http.HandlerFunc) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/_localstack/info", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"version":"4.16.0","edition":"community","is_docker":false,"uptime":42}`))
	})
	for pattern, handler := range extra {
		mux.HandleFunc(pattern, handler)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestAWSCommandUsesExternalInstance is the from-source regression test for
// the head-of-engineering report: `lstk aws` against a LocalStack that is a
// plain process (no container, Docker daemon down) must proxy to it instead of
// failing with "Docker is not available".
func TestAWSCommandUsesExternalInstance(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("fake aws script and unix-socket DOCKER_HOST not supported on Windows")
	}

	srv := mockLocalStackInfoServer(t, nil)
	fakeDir := writeFakeAWS(t)
	e := unhealthyDockerEnv().
		With(env.DisableEvents, "1").
		With("PATH", fakeDir).
		With(env.Home, t.TempDir()).
		With(env.LocalStackHost, lsHost(srv))

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "aws", "s3", "ls")
	require.NoError(t, err, "lstk aws should use the external instance: %s", stderr)

	assert.Contains(t, stdout, "ENDPOINT:http://"+lsHost(srv))
	assert.Contains(t, stdout, "ARGS:s3 ls")
}

// Without LOCALSTACK_HOST, the probe targets the configured port — the
// zero-config from-source case (instance on the config/default port).
func TestAWSCommandUsesExternalInstanceFromConfigPort(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("fake aws script and unix-socket DOCKER_HOST not supported on Windows")
	}

	srv := mockLocalStackInfoServer(t, nil)
	port := srv.URL[strings.LastIndex(srv.URL, ":")+1:]

	workDir := t.TempDir()
	lstkDir := filepath.Join(workDir, ".lstk")
	require.NoError(t, os.MkdirAll(lstkDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(lstkDir, "config.toml"),
		[]byte(fmt.Sprintf("[[containers]]\ntype = \"aws\"\ntag = \"latest\"\nport = %q\n", port)), 0644))

	fakeDir := writeFakeAWS(t)
	e := unhealthyDockerEnv().
		With(env.DisableEvents, "1").
		With("PATH", fakeDir).
		With(env.Home, t.TempDir())

	stdout, stderr, err := runLstk(t, testContext(t), workDir, e, "aws", "s3", "ls")
	require.NoError(t, err, "lstk aws should use the external instance on the configured port: %s", stderr)

	assert.Contains(t, stdout, ":"+port)
	assert.Contains(t, stdout, "ARGS:s3 ls")
}

// Docker healthy but no LocalStack container: discovery falls back to the
// probe instead of erroring.
func TestAWSCommandExternalInstanceWithDockerHealthy(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	srv := mockLocalStackInfoServer(t, nil)
	fakeDir := writeFakeAWS(t)
	e := env.With(env.DisableEvents, "1").
		With("PATH", fakeDir).
		With(env.Home, t.TempDir()).
		With(env.LocalStackHost, lsHost(srv))

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "aws", "s3", "ls")
	require.NoError(t, err, "lstk aws should use the external instance: %s", stderr)

	assert.Contains(t, stdout, "ENDPOINT:http://"+lsHost(srv))
}

func TestTerraformUsesExternalInstance(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("fake terraform script and unix-socket DOCKER_HOST not supported on Windows")
	}

	srv := mockLocalStackInfoServer(t, nil)
	fakeDir := writeFakeTerraform(t)
	e := unhealthyDockerEnv().
		With(env.DisableEvents, "1").
		With("PATH", fakeDir).
		With(env.Home, t.TempDir()).
		With(env.LocalStackHost, lsHost(srv))

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "terraform", "plan")
	require.NoError(t, err, "lstk terraform should use the external instance: %s", stderr)

	assert.Contains(t, stdout, "ARGS:plan")
	assert.Contains(t, stdout, lsHost(srv), "override should point endpoints at the external instance")
}

func TestResetExternalInstance(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("unix-socket DOCKER_HOST not supported on Windows")
	}

	var resetCalls atomic.Int32
	srv := mockLocalStackInfoServer(t, map[string]http.HandlerFunc{
		"/_localstack/state/reset": func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				resetCalls.Add(1)
				w.WriteHeader(http.StatusOK)
				return
			}
			w.WriteHeader(http.StatusMethodNotAllowed)
		},
	})

	e := unhealthyDockerEnv().
		With(env.DisableEvents, "1").
		With(env.Home, t.TempDir()).
		With(env.LocalStackHost, lsHost(srv))

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "--non-interactive", "reset", "--force")
	require.NoError(t, err, "lstk reset should work against the external instance: %s", stderr)

	assert.Contains(t, stdout, "Emulator state reset")
	assert.Equal(t, int32(1), resetCalls.Load())
}

func TestSnapshotSaveExternalInstance(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("unix-socket DOCKER_HOST not supported on Windows")
	}

	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	f, err := zw.Create("state.json")
	require.NoError(t, err)
	_, err = f.Write([]byte(`{"services":{}}`))
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	srv := mockLocalStackInfoServer(t, map[string]http.HandlerFunc{
		"/_localstack/pods/state": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/zip")
			_, _ = w.Write(zipBuf.Bytes())
		},
	})

	dir := t.TempDir()
	e := unhealthyDockerEnv().
		With(env.DisableEvents, "1").
		With(env.Home, t.TempDir()).
		With(env.LocalStackHost, lsHost(srv))

	stdout, stderr, err := runLstk(t, testContext(t), dir, e, "--non-interactive", "snapshot", "save", filepath.Join(dir, "ext.snapshot"))
	require.NoError(t, err, "lstk snapshot save should work against the external instance: %s", stderr)

	assert.Contains(t, stdout, "Snapshot saved")
	_, statErr := os.Stat(filepath.Join(dir, "ext.snapshot"))
	assert.NoError(t, statErr)
}

func TestSnapshotLoadExternalInstance(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("unix-socket DOCKER_HOST not supported on Windows")
	}

	var imported atomic.Bool
	srv := mockLocalStackInfoServer(t, map[string]http.HandlerFunc{
		"/_localstack/pods": func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				imported.Store(true)
				w.WriteHeader(http.StatusOK)
				return
			}
			w.WriteHeader(http.StatusMethodNotAllowed)
		},
	})

	// A minimal zip is a valid snapshot payload for the mock import endpoint.
	dir := t.TempDir()
	snapPath := filepath.Join(dir, "ext.snapshot")
	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	f, err := zw.Create("state.json")
	require.NoError(t, err)
	_, err = f.Write([]byte(`{"services":{}}`))
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	require.NoError(t, os.WriteFile(snapPath, zipBuf.Bytes(), 0644))

	e := unhealthyDockerEnv().
		With(env.DisableEvents, "1").
		With(env.Home, t.TempDir()).
		With(env.LocalStackHost, lsHost(srv))

	stdout, stderr, err := runLstk(t, testContext(t), dir, e, "--non-interactive", "snapshot", "load", snapPath)
	require.NoError(t, err, "lstk snapshot load should work against the external instance: %s", stderr)

	assert.Contains(t, stdout, "Snapshot loaded")
	assert.True(t, imported.Load(), "import endpoint should be called")
}

func TestAzCommandUsesExternalInstance(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("fake az script and unix-socket DOCKER_HOST not supported on Windows")
	}

	srv := mockLocalStackInfoServer(t, nil)
	workDir := azureWorkDir(t)
	writeAzureSetupMarker(t, workDir)

	fakeDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(fakeDir, "az"),
		[]byte("#!/bin/sh\necho \"AZ-ARGS:$*\"\n"), 0755))

	e := unhealthyDockerEnv().
		With(env.DisableEvents, "1").
		With("PATH", fakeDir).
		With(env.Home, t.TempDir()).
		With(env.LocalStackHost, lsHost(srv))

	stdout, stderr, err := runLstk(t, testContext(t), workDir, e, "az", "group", "list")
	require.NoError(t, err, "lstk az should use the external instance: %s", stderr)

	assert.Contains(t, stdout, "AZ-ARGS:group list")
}
