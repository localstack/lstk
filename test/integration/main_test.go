package integration_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
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

	// Force file backend if KEYRING env var is set to "file"
	if os.Getenv("KEYRING") == "file" {
		keyringConfig.AllowedBackends = []keyring.BackendType{keyring.FileBackend}
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

func envWithout(keys ...string) []string {
	var env []string
	for _, e := range os.Environ() {
		excluded := false
		for _, key := range keys {
			if strings.HasPrefix(e, key+"=") {
				excluded = true
				break
			}
		}
		if !excluded {
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
