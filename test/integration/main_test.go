package integration_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"
)

// syncBuffer is a thread-safe buffer for concurrent read/write access.
type syncBuffer struct {
	buf bytes.Buffer
	mu  sync.Mutex
}

func (b *syncBuffer) Write(p []byte) (n int, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Bytes()
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

const (
	keyringService      = "lstk"
	keyringAuthTokenKey = "lstk.auth-token"
	authTokenFile       = "auth-token"
)

func binaryPath() string {
	if runtime.GOOS == "windows" {
		return "../../bin/lstk.exe"
	}
	return "../../bin/lstk"
}

var dockerClient *client.Client
var dockerAvailable bool
var useFileKeyring bool

// configDir returns the lstk config directory.
// Duplicated from internal/config to avoid importing prod code in tests.
func configDir() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		panic(fmt.Sprintf("failed to get user home directory: %v", err))
	}
	homeConfigDir := filepath.Join(homeDir, ".config")
	if info, err := os.Stat(homeConfigDir); err == nil && info.IsDir() {
		return filepath.Join(homeConfigDir, "lstk")
	}

	configHome, err := os.UserConfigDir()
	if err != nil {
		panic(fmt.Sprintf("failed to get user config directory: %v", err))
	}
	return filepath.Join(configHome, "lstk")
}

func TestMain(m *testing.M) {
	var err error
	dockerClient, err = client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err == nil {
		_, err = dockerClient.Ping(context.Background())
		dockerAvailable = err == nil
	}

	// Determine whether to use file-based keyring: forced via env var or
	// probed by attempting a system keyring read.
	if env.Get(env.Keyring) == "file" {
		useFileKeyring = true
	} else {
		_, err := keyring.Get(keyringService, keyringAuthTokenKey)
		if err != nil && !errors.Is(err, keyring.ErrNotFound) {
			useFileKeyring = true
		}
	}

	m.Run()
}

func requireDocker(t *testing.T) {
	t.Helper()
	if !dockerAvailable {
		t.Skip("Docker is not available")
	}
	// Skip Docker tests on Windows (GitHub Actions doesn't support Linux containers)
	// Note: CI env var is not in config.GetEnv() as it's a standard CI environment variable
	if runtime.GOOS == "windows" && env.Get(env.CI) != "" {
		t.Skip("Docker tests not supported on Windows CI (nested virtualization not available)")
	}
}

func GetAuthTokenFromKeyring() (string, error) {
	if useFileKeyring {
		data, err := os.ReadFile(filepath.Join(configDir(), authTokenFile))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return "", fmt.Errorf("credential not found")
			}
			return "", err
		}
		return string(data), nil
	}
	token, err := keyring.Get(keyringService, keyringAuthTokenKey)
	if err != nil {
		return "", err
	}
	return token, nil
}

func SetAuthTokenInKeyring(password string) error {
	if useFileKeyring {
		dir := configDir()
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, authTokenFile), []byte(password), 0600)
	}
	return keyring.Set(keyringService, keyringAuthTokenKey, password)
}

func DeleteAuthTokenFromKeyring() error {
	if useFileKeyring {
		err := os.Remove(filepath.Join(configDir(), authTokenFile))
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	err := keyring.Delete(keyringService, keyringAuthTokenKey)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

const (
	containerName = "localstack-aws"
	testImage     = "alpine:latest"
)

func startTestContainer(t *testing.T, ctx context.Context) {
	t.Helper()

	reader, err := dockerClient.ImagePull(ctx, testImage, image.PullOptions{})
	require.NoError(t, err, "failed to pull test image")
	_, _ = io.Copy(io.Discard, reader)
	_ = reader.Close()

	cfg := &container.Config{
		Image: testImage,
		Cmd:   []string{"sleep", "infinity"},
	}

	resp, err := dockerClient.ContainerCreate(ctx, cfg, nil, nil, nil, containerName)
	require.NoError(t, err, "failed to create test container")
	err = dockerClient.ContainerStart(ctx, resp.ID, container.StartOptions{})
	require.NoError(t, err, "failed to start test container")
}

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	return ctx
}

func runLstk(t *testing.T, ctx context.Context, dir string, env []string, args ...string) (string, string, error) {
	t.Helper()

	binPath, err := filepath.Abs(binaryPath())
	require.NoError(t, err)

	cmd := exec.CommandContext(ctx, binPath, args...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = env
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	return strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()), err
}

// runs the lstk binary inside a PTY so that ui.IsInteractive() returns true,
// making --non-interactive the actual condition under test
func runLstkInPTY(t *testing.T, ctx context.Context, environ []string, args ...string) (string, error) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("PTY not supported on Windows")
	}

	binPath, err := filepath.Abs(binaryPath())
	require.NoError(t, err)

	cmd := exec.CommandContext(ctx, binPath, args...)
	if environ != nil {
		cmd.Env = environ
	}

	ptmx, err := pty.Start(cmd)
	require.NoError(t, err, "failed to start command in PTY")
	defer func() { _ = ptmx.Close() }()

	out := &syncBuffer{}
	outputCh := make(chan struct{})
	go func() {
		_, _ = io.Copy(out, ptmx)
		close(outputCh)
	}()

	err = cmd.Wait()
	<-outputCh
	return strings.TrimSpace(out.String()), err
}

func requireExitCode(t *testing.T, expected int, err error) {
	t.Helper()
	if expected == 0 {
		require.NoError(t, err)
		return
	}
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	require.Equal(t, expected, exitErr.ExitCode())
}

func createMockLicenseServer(success bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/v1/license/request" {
			if success {
				w.WriteHeader(http.StatusOK)
			} else {
				w.WriteHeader(http.StatusForbidden)
			}
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
}
