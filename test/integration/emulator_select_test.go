package integration_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNoEmulatorSelectionWhenConfigExists(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("PTY not supported on Windows")
	}

	tmpHome := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmpHome, ".config"), 0755))
	e := env.Environ(testEnvWithHome(tmpHome, tmpHome)).
		With(env.DisableEvents, "1")

	// Pre-create the config so lstk does not treat this as a first run.
	configPath, _, err := runLstk(t, testContext(t), "", e, "config", "path")
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0755))
	require.NoError(t, os.WriteFile(configPath, []byte("[[containers]]\ntype = \"aws\"\ntag = \"latest\"\nport = \"4566\"\n"), 0644))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath(), "start")
	cmd.Env = e

	ptmx, err := pty.Start(cmd)
	require.NoError(t, err, "failed to start lstk in PTY")
	defer func() { _ = ptmx.Close() }()

	out := &syncBuffer{}
	outputCh := make(chan struct{})
	go func() {
		_, _ = io.Copy(out, ptmx)
		close(outputCh)
	}()

	assert.Never(t, func() bool {
		return bytes.Contains(out.Bytes(), []byte("Which emulator would you like to use?"))
	}, 2*time.Second, 100*time.Millisecond, "emulator selection prompt should not appear when config already exists")

	cancel()
	<-outputCh
}

func TestFirstRunShowsEmulatorSelectionPrompt(t *testing.T) {
	requireDocker(t)
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("PTY not supported on Windows")
	}

	tmpHome := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmpHome, ".config"), 0755))
	e := env.Environ(testEnvWithHome(tmpHome, tmpHome)).
		With(env.DisableEvents, "1")

	// Confirm no config exists at the path lstk would use — this is what triggers first-run.
	configPath, _, err := runLstk(t, testContext(t), "", e, "config", "path")
	require.NoError(t, err)
	require.NoFileExists(t, configPath)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath(), "start")
	cmd.Env = e

	ptmx, err := pty.Start(cmd)
	require.NoError(t, err, "failed to start lstk in PTY")
	defer func() { _ = ptmx.Close() }()

	out := &syncBuffer{}
	outputCh := make(chan struct{})
	go func() {
		_, _ = io.Copy(out, ptmx)
		close(outputCh)
	}()

	require.Eventually(t, func() bool {
		return bytes.Contains(out.Bytes(), []byte("Which emulator would you like to use?"))
	}, 10*time.Second, 100*time.Millisecond, "emulator selection prompt should appear on first run")

	// Confirm the default-highlighted option (AWS) by pressing Enter.
	_, err = ptmx.Write([]byte("\r"))
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return bytes.Contains(out.Bytes(), []byte("AWS emulator selected."))
	}, 10*time.Second, 100*time.Millisecond, "selection confirmation should appear after pressing Enter")

	// SetEmulatorType writes the config before emitting the confirmation message,
	// so the file is guaranteed to exist and contain the selection by this point.
	configData, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Contains(t, string(configData), `type = "aws"`)

	cancel()
	<-outputCh
}

func TestFirstRunCanSelectAzureEmulator(t *testing.T) {
	requireDocker(t)
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("PTY not supported on Windows")
	}

	tmpHome := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmpHome, ".config"), 0755))
	e := env.Environ(testEnvWithHome(tmpHome, tmpHome)).
		With(env.DisableEvents, "1")

	configPath, _, err := runLstk(t, testContext(t), "", e, "config", "path")
	require.NoError(t, err)
	require.NoFileExists(t, configPath)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath(), "start")
	cmd.Env = e

	ptmx, err := pty.Start(cmd)
	require.NoError(t, err, "failed to start lstk in PTY")
	defer func() { _ = ptmx.Close() }()

	out := &syncBuffer{}
	outputCh := make(chan struct{})
	go func() {
		_, _ = io.Copy(out, ptmx)
		close(outputCh)
	}()

	require.Eventually(t, func() bool {
		return bytes.Contains(out.Bytes(), []byte("Which emulator would you like to use?"))
	}, 10*time.Second, 100*time.Millisecond, "emulator selection prompt should appear on first run")

	assert.Contains(t, out.String(), "Azure", "Azure should be offered as a selectable emulator")

	// Press the Azure selection key ('z') instead of the default-highlighted AWS.
	_, err = ptmx.Write([]byte("z"))
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return bytes.Contains(out.Bytes(), []byte("Azure emulator selected."))
	}, 10*time.Second, 100*time.Millisecond, "Azure selection confirmation should appear")

	configData, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Contains(t, string(configData), `type = "azure"`)

	cancel()
	<-outputCh
}

func TestFirstRunPromptsForLoginBeforeEmulatorSelection(t *testing.T) {
	requireDocker(t)
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("PTY not supported on Windows")
	}

	mockServer := createMockAPIServer(t, "test-license-token", true)
	defer mockServer.Close()

	tmpHome := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmpHome, ".config"), 0755))
	e := env.Environ(testEnvWithHome(tmpHome, tmpHome)).
		Without(env.AuthToken).
		With(env.APIEndpoint, mockServer.URL).
		With(env.DisableEvents, "1")

	// No config exists so this is a first run; no token means login fires before emulator selection.
	configPath, _, err := runLstk(t, testContext(t), "", e, "config", "path")
	require.NoError(t, err)
	require.NoFileExists(t, configPath)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath(), "start")
	cmd.Env = e

	ptmx, err := pty.Start(cmd)
	require.NoError(t, err, "failed to start lstk in PTY")
	defer func() { _ = ptmx.Close() }()

	out := &syncBuffer{}
	outputCh := make(chan struct{})
	go func() {
		_, _ = io.Copy(out, ptmx)
		close(outputCh)
	}()

	require.Eventually(t, func() bool {
		return bytes.Contains(out.Bytes(), []byte("Press any key when complete"))
	}, 10*time.Second, 100*time.Millisecond, "auth prompt should appear on first run when no token is set")

	assert.NotContains(t, out.String(), "Which emulator would you like to use?",
		"emulator selection prompt must not appear before auth completes")

	_, err = ptmx.Write([]byte("\r"))
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return bytes.Contains(out.Bytes(), []byte("Which emulator would you like to use?"))
	}, 10*time.Second, 100*time.Millisecond, "emulator selection prompt should appear after auth completes")

	cancel()
	<-outputCh
}

func TestFirstRunNonInteractiveEmitsDefaultEmulatorNote(t *testing.T) {
	t.Parallel()
	tmpHome := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmpHome, ".config"), 0755))
	e := env.Environ(testEnvWithHome(tmpHome, tmpHome)).With(env.DisableEvents, "1")

	// Verify no config exists — this is what triggers first-run.
	configPath, _, err := runLstk(t, testContext(t), "", e, "config", "path")
	require.NoError(t, err)
	require.NoFileExists(t, configPath)

	// Process fails at container.Start (no Docker), but the note is emitted before that.
	stdout, _, runErr := runLstk(t, testContext(t), "", e.With(env.AuthToken, "test-token"), "--non-interactive")
	assert.Error(t, runErr, "expected failure: no Docker available")
	assert.Contains(t, stdout, "Configured with default emulator", "non-interactive first run should note the default emulator")
}

func TestFirstRunChecksDockerBeforeAuthAndSelection(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("PTY/Unix socket test")
	}

	mockServer := createMockAPIServer(t, "test-license-token", true)
	defer mockServer.Close()

	tmpHome := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmpHome, ".config"), 0755))
	e := env.Environ(testEnvWithHome(tmpHome, tmpHome)).
		Without(env.AuthToken).
		With(env.APIEndpoint, mockServer.URL).
		With(env.DisableEvents, "1").
		With(env.Key("DOCKER_HOST"), "unix:///var/run/docker-does-not-exist.sock")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	out, err := runLstkInPTY(t, ctx, e, "start")
	require.Error(t, err)
	requireExitCode(t, 1, err)
	assert.Contains(t, out, "Docker is not available")
	assert.NotContains(t, out, "Press any key when complete",
		"login prompt must not appear when the runtime is unavailable")
	assert.NotContains(t, out, "Which emulator would you like to use?",
		"emulator selection must not appear when the runtime is unavailable")
}
