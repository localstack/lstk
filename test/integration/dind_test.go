package integration_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/require"
)

const dindImage = "docker:dind"

// testDaemon is a per-test ephemeral Docker daemon (Docker-in-Docker).
// Each test gets its own kernel namespaces (network, mount, pid), so port
// bindings, container names, and docker state are fully isolated. lstk
// subprocesses point at the daemon via env.DockerHost.
type testDaemon struct {
	Host   string         // tcp://127.0.0.1:<port>, suitable for DOCKER_HOST
	Port   int            // host-side port that maps to the daemon's 2375
	Client *client.Client // for direct introspection from the test process
}

// startEphemeralDocker boots a docker:dind container on the host's daemon
// (the one TestMain connected to), waits for the inner daemon to accept
// connections, and registers cleanup. Pre-cached host images (alpine, and
// optionally localstack-pro/snowflake) are loaded into the new daemon so
// tests don't pay network-pull cost per run.
func startEphemeralDocker(t *testing.T, preload ...string) *testDaemon {
	t.Helper()

	ensureDindImage(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	port, err := freeTCPPort()
	require.NoError(t, err, "failed to allocate free port for dind")

	name := "lstk-dind-" + randomID(t)

	resp, err := dockerClient.ContainerCreate(ctx,
		&container.Config{
			Image:        dindImage,
			Cmd:          []string{"dockerd", "--host=tcp://0.0.0.0:2375", "--tls=false"},
			Env:          []string{"DOCKER_TLS_CERTDIR="},
			ExposedPorts: nat.PortSet{"2375/tcp": struct{}{}},
		},
		&container.HostConfig{
			Privileged: true,
			PortBindings: nat.PortMap{
				"2375/tcp": []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: strconv.Itoa(port)}},
			},
			Tmpfs: map[string]string{"/var/lib/docker": ""}, // ephemeral, faster teardown
		},
		nil, nil, name,
	)
	require.NoError(t, err, "failed to create dind container")

	require.NoError(t, dockerClient.ContainerStart(ctx, resp.ID, container.StartOptions{}),
		"failed to start dind container")

	t.Cleanup(func() {
		_ = dockerClient.ContainerRemove(context.Background(), resp.ID, container.RemoveOptions{Force: true})
	})

	host := fmt.Sprintf("tcp://127.0.0.1:%d", port)
	inner, err := client.NewClientWithOpts(
		client.WithHost(host),
		client.WithAPIVersionNegotiation(),
	)
	require.NoError(t, err, "failed to construct dind client")

	require.Eventually(t, func() bool {
		pingCtx, pingCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer pingCancel()
		_, err := inner.Ping(pingCtx)
		return err == nil
	}, 30*time.Second, 200*time.Millisecond, "dind daemon should accept connections")

	d := &testDaemon{Host: host, Port: port, Client: inner}

	// Always preload alpine — used by every stub container helper.
	loadCachedImage(t, d, testImage)
	for _, img := range preload {
		loadCachedImage(t, d, img)
	}

	return d
}

// ensureDindImage pulls docker:dind into the host daemon once if missing.
// Subsequent tests reuse the cached image.
func ensureDindImage(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if _, err := dockerClient.ImageInspect(ctx, dindImage); err == nil {
		return
	}
	reader, err := dockerClient.ImagePull(ctx, dindImage, image.PullOptions{})
	require.NoError(t, err, "failed to pull dind image")
	defer func() { _ = reader.Close() }()
	_, _ = io.Copy(io.Discard, reader)
}

// imageCache stores tar files of host images keyed by image name, so a single
// host-side ImageSave is reused across every test's daemon.
type imageCache struct {
	mu    sync.Mutex
	dir   string
	paths map[string]string
}

var hostImages = &imageCache{paths: make(map[string]string)}

// imageCachePath returns a path to a tar of the named image, performing the
// host-side ImageSave on first request and caching the bytes on disk.
// The tarball's parent dir lives for the life of the test process.
func imageCachePath(t *testing.T, name string) string {
	t.Helper()
	hostImages.mu.Lock()
	defer hostImages.mu.Unlock()

	if p, ok := hostImages.paths[name]; ok {
		return p
	}
	if hostImages.dir == "" {
		dir, err := os.MkdirTemp("", "lstk-image-cache-")
		require.NoError(t, err, "failed to create image cache dir")
		hostImages.dir = dir
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Pull on demand if missing on the host.
	if _, err := dockerClient.ImageInspect(ctx, name); err != nil {
		reader, err := dockerClient.ImagePull(ctx, name, image.PullOptions{})
		require.NoError(t, err, "failed to pull %s on host", name)
		_, _ = io.Copy(io.Discard, reader)
		_ = reader.Close()
	}

	saver, err := dockerClient.ImageSave(ctx, []string{name})
	require.NoError(t, err, "failed to save %s on host", name)
	defer func() { _ = saver.Close() }()

	tarPath := filepath.Join(hostImages.dir, sanitizeImageName(name)+".tar")
	f, err := os.Create(tarPath)
	require.NoError(t, err, "failed to create image cache file")
	defer func() { _ = f.Close() }()
	_, err = io.Copy(f, saver)
	require.NoError(t, err, "failed to write image cache")

	hostImages.paths[name] = tarPath
	return tarPath
}

// loadCachedImage streams a previously-saved host image into the dind daemon.
func loadCachedImage(t *testing.T, daemon *testDaemon, name string) {
	t.Helper()
	tarPath := imageCachePath(t, name)
	f, err := os.Open(tarPath)
	require.NoError(t, err, "failed to open cached image %s", name)
	defer func() { _ = f.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	resp, err := daemon.Client.ImageLoad(ctx, f)
	require.NoError(t, err, "failed to load %s into dind", name)
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
}

func sanitizeImageName(name string) string {
	out := make([]byte, 0, len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c == '/' || c == ':' {
			c = '_'
		}
		out = append(out, c)
	}
	return string(out)
}

// freeTCPPort asks the kernel for an available TCP port on 127.0.0.1.
// There's an unavoidable race window between Close() and the dind container
// binding the port, but it's small in practice.
func freeTCPPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// randomID returns a short random hex string suitable for container/test names.
func randomID(t *testing.T) string {
	t.Helper()
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("failed to generate random id: %v", err)
	}
	return hex.EncodeToString(b[:])
}

// startStubInDind creates a sleep-infinity container inside the dind daemon
// so lstk has something to attach to / inspect / log without booting real
// LocalStack. Optional hostPort binds container 4566/tcp inside the daemon's
// network namespace. (That port is not exposed to the host; only the dind
// daemon socket is.)
func startStubInDind(t *testing.T, daemon *testDaemon, name string, hostPort ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cfg := &container.Config{
		Image: testImage,
		Cmd:   []string{"sleep", "infinity"},
	}
	var hostCfg *container.HostConfig
	if len(hostPort) > 0 {
		const containerPort = nat.Port("4566/tcp")
		cfg.ExposedPorts = nat.PortSet{containerPort: struct{}{}}
		hostCfg = &container.HostConfig{
			PortBindings: nat.PortMap{
				containerPort: []nat.PortBinding{{HostPort: hostPort[0]}},
			},
		}
	}

	resp, err := daemon.Client.ContainerCreate(ctx, cfg, hostCfg, nil, nil, name)
	require.NoError(t, err, "failed to create stub container in dind")
	require.NoError(t, daemon.Client.ContainerStart(ctx, resp.ID, container.StartOptions{}))
}

// envWithDockerHost returns a process environment with DOCKER_HOST pointing at
// the given daemon, suitable for chaining .With() calls before passing to runLstk.
//
// HOME is also pointed at a fresh tempdir so the lstk subprocess never reads
// the developer's real ~/.config/lstk/config.toml (default aws config gets
// auto-created in the tempdir on first run).
func envWithDockerHost(t *testing.T, daemon *testDaemon) env.Environ {
	t.Helper()
	return env.Environ(os.Environ()).
		With(env.Key("DOCKER_HOST"), daemon.Host).
		With(env.Home, t.TempDir())
}

// startExternalInDind simulates a LocalStack container started outside lstk,
// inside the daemon's namespace. The host port binding is taken inside dind's
// network namespace so it doesn't collide across parallel tests.
func startExternalInDind(t *testing.T, daemon *testDaemon, imgName, name, hostPort string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const containerPort = nat.Port("4566/tcp")
	resp, err := daemon.Client.ContainerCreate(ctx,
		&container.Config{
			Image:        imgName,
			Cmd:          []string{"sleep", "infinity"},
			ExposedPorts: nat.PortSet{containerPort: struct{}{}},
		},
		&container.HostConfig{
			PortBindings: nat.PortMap{
				containerPort: []nat.PortBinding{{HostPort: hostPort}},
			},
		},
		nil, nil, name,
	)
	require.NoError(t, err, "failed to create external container in dind")
	require.NoError(t, daemon.Client.ContainerStart(ctx, resp.ID, container.StartOptions{}))
}

// TestDindProofOfConcept verifies lstk can drive an ephemeral per-test dind:
// the stub container is created inside dind, lstk logs reads from it via
// DOCKER_HOST. The host docker is untouched.
func TestDindProofOfConcept(t *testing.T) {
	requireDocker(t)
	t.Parallel()

	daemon := startEphemeralDocker(t)
	startStubInDind(t, daemon, containerName)

	configFile := writeAwsConfig(t)
	_, _, err := runLstk(t, testContext(t), "", envWithDockerHost(t, daemon), "--config", configFile, "logs")
	require.NoError(t, err, "lstk logs against dind should succeed")
	requireExitCode(t, 0, err)
}

// TestDindParallelStress launches several daemons concurrently to confirm that
// per-test isolation holds: each test's stub container has the same name
// (localstack-aws) but lives in its own daemon, so no Docker name collision.
func TestDindParallelStress(t *testing.T) {
	requireDocker(t)
	for i := 0; i < 5; i++ {
		t.Run(fmt.Sprintf("dind-%d", i), func(t *testing.T) {
			t.Parallel()
			daemon := startEphemeralDocker(t)
			startStubInDind(t, daemon, containerName)

			configFile := writeAwsConfig(t)
			_, _, err := runLstk(t, testContext(t), "", envWithDockerHost(t, daemon), "--config", configFile, "logs")
			require.NoError(t, err, "lstk logs against dind should succeed")
		})
	}
}
