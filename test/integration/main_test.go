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

	"net/netip"

	"github.com/creack/pty"
	"github.com/localstack/lstk/test/integration/env"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
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
	dockerClient, err = client.New(client.FromEnv)
	if err == nil {
		_, err = dockerClient.Ping(context.Background(), client.PingOptions{})
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

// startTestContainer starts the test container with no port bindings by default.
// Pass hostPort to bind 4566/tcp to a specific host port (e.g. to test that lstk status
// uses the actual bound port rather than the port from config).
func startTestContainer(t *testing.T, ctx context.Context, hostPort ...string) {
	t.Helper()

	reader, err := dockerClient.ImagePull(ctx, testImage, client.ImagePullOptions{})
	require.NoError(t, err, "failed to pull test image")
	_, _ = io.Copy(io.Discard, reader)
	_ = reader.Close()

	cfg := &container.Config{
		Image: testImage,
		Cmd:   []string{"sleep", "infinity"},
	}
	var hostCfg *container.HostConfig
	if len(hostPort) > 0 {
		containerPort := network.MustParsePort("4566/tcp")
		cfg.ExposedPorts = network.PortSet{containerPort: struct{}{}}
		hostCfg = &container.HostConfig{
			PortBindings: network.PortMap{
				// 127.0.0.2 avoids conflicting with the mock HTTP server on 127.0.0.1:hostPort.
				containerPort: []network.PortBinding{{HostIP: netip.MustParseAddr("127.0.0.2"), HostPort: hostPort[0]}},
			},
		}
	}

	resp, err := dockerClient.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:     cfg,
		HostConfig: hostCfg,
		Name:       containerName,
	})
	require.NoError(t, err, "failed to create test container")
	_, err = dockerClient.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{})
	require.NoError(t, err, "failed to start test container")
}

// Use this to simulate a LocalStack container started outside lstk.
func startExternalContainer(t *testing.T, ctx context.Context, imgName, name, hostPort string) {
	t.Helper()

	containerPort := network.MustParsePort("4566/tcp")
	resp, err := dockerClient.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{
			Image:        imgName,
			Cmd:          []string{"sleep", "infinity"},
			ExposedPorts: network.PortSet{containerPort: struct{}{}},
		},
		HostConfig: &container.HostConfig{
			PortBindings: network.PortMap{
				containerPort: []network.PortBinding{{HostPort: hostPort}},
			},
		},
		Name: name,
	})
	require.NoError(t, err, "failed to create external container")
	_, err = dockerClient.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{})
	require.NoError(t, err, "failed to start external container")
	t.Cleanup(func() {
		_, _ = dockerClient.ContainerRemove(context.Background(), name, client.ContainerRemoveOptions{Force: true})
	})
}

// commitNeverHealthyImage builds a local-only image whose default command stays
// running (sleep infinity) but never serves /_localstack/health. Starting it via
// lstk exercises the failure path where the emulator comes up but never reports
// healthy. Returns the image reference; the image and its source container are
// removed on test cleanup.
func commitNeverHealthyImage(t *testing.T, ctx context.Context) string {
	t.Helper()

	reader, err := dockerClient.ImagePull(ctx, testImage, client.ImagePullOptions{})
	require.NoError(t, err, "failed to pull test image")
	_, _ = io.Copy(io.Discard, reader)
	_ = reader.Close()

	resp, err := dockerClient.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{Image: testImage},
		Name:   "lstk-never-healthy-src",
	})
	require.NoError(t, err, "failed to create source container")
	t.Cleanup(func() {
		_, _ = dockerClient.ContainerRemove(context.Background(), resp.ID, client.ContainerRemoveOptions{Force: true})
	})

	const imageRef = "lstk-never-healthy:latest"
	_, err = dockerClient.ContainerCommit(ctx, resp.ID, client.ContainerCommitOptions{
		Reference: imageRef,
		Changes:   []string{`CMD ["sleep", "infinity"]`},
	})
	require.NoError(t, err, "failed to commit never-healthy image")
	t.Cleanup(func() {
		_, _ = dockerClient.ImageRemove(context.Background(), imageRef, client.ImageRemoveOptions{Force: true})
	})
	return imageRef
}

func startTestSnowflakeContainer(t *testing.T, ctx context.Context) {
	t.Helper()
	startNamedTestContainer(t, ctx, snowflakeContainerName, "snowflake")
}

func startTestAzureContainer(t *testing.T, ctx context.Context) {
	t.Helper()
	startNamedTestContainer(t, ctx, azureContainerName, "azure")
}

// startNamedTestContainer starts a placeholder container (sleep infinity) under
// the given name so emulator running-detection matches it by image and port.
func startNamedTestContainer(t *testing.T, ctx context.Context, name, label string) {
	t.Helper()

	reader, err := dockerClient.ImagePull(ctx, testImage, client.ImagePullOptions{})
	require.NoError(t, err, "failed to pull test image")
	_, _ = io.Copy(io.Discard, reader)
	_ = reader.Close()

	resp, err := dockerClient.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{
			Image: testImage,
			Cmd:   []string{"sleep", "infinity"},
		},
		Name: name,
	})
	require.NoError(t, err, "failed to create %s test container", label)
	_, err = dockerClient.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{})
	require.NoError(t, err, "failed to start %s test container", label)
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
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"license_type":"ultimate"}`))
			} else {
				w.WriteHeader(http.StatusForbidden)
			}
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
}

func createMockLicenseServerWithBody(body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/v1/license/request" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(body))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
}
