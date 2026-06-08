package integration_test

import (
	"context"
	"io"
	"net/http"
	"net/netip"
	"testing"
	"time"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

// Helpers for e2e tests that need a *real* LocalStack — one that serves the AWS
// APIs — rather than the alpine stand-in (startTestContainer) most integration
// tests use for discovery/health only. Keep these reusable across e2e suites
// (terraform, and future ones) instead of duplicating the bring-up logic.

// requireAuthToken returns LOCALSTACK_AUTH_TOKEN or skips the test. The real
// LocalStack image needs it to activate, so these tests can't run without it.
func requireAuthToken(t *testing.T) string {
	t.Helper()
	token := env.Get(env.AuthToken)
	if token == "" {
		t.Skip("LOCALSTACK_AUTH_TOKEN not set")
	}
	return token
}

// startRealLocalStack runs a real LocalStack emulator under the given image and
// container name, with 4566 bound to 127.0.0.1 — the address lstk resolves the
// endpoint to — so a host-side subprocess (e.g. terraform) can reach it, named
// so lstk's discovery finds it. The auth token activates the image. It waits for
// the health endpoint before returning. Callers are responsible for removing the
// container (e.g. cleanup() for the AWS emulator's "localstack-aws").
//
// image and name vary per emulator (e.g. localstack/localstack-pro + localstack-aws
// for AWS, localstack/localstack-azure + localstack-azure for Azure); the edge
// port (4566) and health path are uniform across emulators.
func startRealLocalStack(t *testing.T, ctx context.Context, image, name, token string) {
	t.Helper()

	reader, err := dockerClient.ImagePull(ctx, image, client.ImagePullOptions{})
	require.NoError(t, err, "failed to pull %s", image)
	_, _ = io.Copy(io.Discard, reader)
	_ = reader.Close()

	port := network.MustParsePort("4566/tcp")
	resp, err := dockerClient.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{
			Image:        image,
			Env:          []string{"LOCALSTACK_AUTH_TOKEN=" + token},
			ExposedPorts: network.PortSet{port: struct{}{}},
		},
		HostConfig: &container.HostConfig{
			PortBindings: network.PortMap{
				port: []network.PortBinding{{HostIP: netip.MustParseAddr("127.0.0.1"), HostPort: "4566"}},
			},
		},
		Name: name,
	})
	require.NoError(t, err, "failed to create %s container", name)
	_, err = dockerClient.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{})
	require.NoError(t, err, "failed to start %s container", name)

	waitForLocalStackReady(t, ctx)
}

// waitForLocalStackReady polls the LocalStack health endpoint until it returns
// 200 or the timeout elapses.
func waitForLocalStackReady(t *testing.T, ctx context.Context) {
	t.Helper()
	const url = "http://127.0.0.1:4566/_localstack/health"
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(time.Second)
	}
	t.Fatal("LocalStack did not become ready within 120s")
}
