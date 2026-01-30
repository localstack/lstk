package integration_test

import (
	"testing"

	"github.com/docker/docker/client"
)

var dockerClient *client.Client

func TestMain(m *testing.M) {
	var err error
	dockerClient, err = client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		panic(err)
	}
	m.Run()
}
