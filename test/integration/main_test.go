package integration_test

import (
	"context"
	"runtime"
	"testing"

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

func TestMain(m *testing.M) {
	var err error
	dockerClient, err = client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err == nil {
		_, err = dockerClient.Ping(context.Background())
		dockerAvailable = err == nil
	}
	m.Run()
}

func requireDocker(t *testing.T) {
	t.Helper()
	if !dockerAvailable {
		t.Skip("Docker is not available")
	}
}
