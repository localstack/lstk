package integration_test

import (
	"context"
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
var testKeyring keyring.Keyring

func TestMain(m *testing.M) {
	var err error
	dockerClient, err = client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err == nil {
		_, err = dockerClient.Ping(context.Background())
		dockerAvailable = err == nil
	}

	configDir, _ := os.UserConfigDir()
	config := keyring.Config{
		ServiceName: "localstack",
		FileDir:     filepath.Join(configDir, "localstack"),
		FilePasswordFunc: func(prompt string) (string, error) {
			return "localstack-keyring", nil
		},
	}

	testKeyring, err = keyring.Open(config)
	if err != nil {
		config.AllowedBackends = []keyring.BackendType{keyring.FileBackend}
		testKeyring, _ = keyring.Open(config)
	}

	m.Run()
}

func requireDocker(t *testing.T) {
	t.Helper()
	if !dockerAvailable {
		t.Skip("Docker is not available")
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
	item, err := testKeyring.Get(key)
	if err != nil {
		return "", err
	}
	return string(item.Data), nil
}

func keyringSet(service, user, password string) error {
	key := fmt.Sprintf("%s/%s", service, user)
	return testKeyring.Set(keyring.Item{
		Key:  key,
		Data: []byte(password),
	})
}

func keyringDelete(service, user string) error {
	key := fmt.Sprintf("%s/%s", service, user)
	err := testKeyring.Remove(key)
	if err == keyring.ErrKeyNotFound || os.IsNotExist(err) {
		return nil
	}
	return err
}
