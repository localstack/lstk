package integration_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/99designs/keyring"
	"github.com/docker/docker/client"
)

func binaryPath() string {
	if runtime.GOOS == "windows" {
		return "../../bin/lstk.exe"
	}
	return "../../bin/lstk"
}

var dockerClient *client.Client
var dockerAvailable bool
var ring keyring.Keyring

// configDir returns the lstk config directory.
// Duplicated from internal/config to avoid importing prod code in tests.
func configDir() string {
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

	keyringConfig := keyring.Config{
		ServiceName: "localstack",
		FileDir:     filepath.Join(configDir(), "keyring"),
		FilePasswordFunc: func(prompt string) (string, error) {
			return "localstack-keyring", nil
		},
	}

	ring, err = keyring.Open(keyringConfig)
	if err != nil {
		keyringConfig.AllowedBackends = []keyring.BackendType{keyring.FileBackend}
		ring, _ = keyring.Open(keyringConfig)
	}

	m.Run()
}

func requireDocker(t *testing.T) {
	t.Helper()
	if !dockerAvailable {
		t.Skip("Docker is not available")
	}
	// Skip Docker tests on Windows (GitHub Actions doesn't support Linux containers)
	if runtime.GOOS == "windows" && os.Getenv("CI") != "" {
		t.Skip("Docker tests not supported on Windows CI (nested virtualization not available)")
	}
}

func envWithoutAuthToken() []string {
	var env []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "LOCALSTACK_AUTH_TOKEN=") {
			env = append(env, e)
		}
	}
	return env
}

func keyringGet(service, user string) (string, error) {
	key := fmt.Sprintf("%s/%s", service, user)
	item, err := ring.Get(key)
	if err != nil {
		return "", err
	}
	return string(item.Data), nil
}

func keyringSet(service, user, password string) error {
	key := fmt.Sprintf("%s/%s", service, user)
	return ring.Set(keyring.Item{
		Key:  key,
		Data: []byte(password),
	})
}

func keyringDelete(service, user string) error {
	key := fmt.Sprintf("%s/%s", service, user)
	err := ring.Remove(key)
	if errors.Is(err, keyring.ErrKeyNotFound) || os.IsNotExist(err) {
		return nil
	}
	return err
}
