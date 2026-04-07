package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	stdruntime "runtime"
	"strconv"
	"strings"
	"time"

	"github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
	"github.com/localstack/lstk/internal/output"
)

// DockerRuntime implements Runtime using the Docker API.
type DockerRuntime struct {
	client *client.Client
}

func NewDockerRuntime(dockerHost string) (*DockerRuntime, error) {
	opts := []client.Opt{client.FromEnv, client.WithAPIVersionNegotiation()}

	// When DOCKER_HOST is not set, the Docker SDK defaults to /var/run/docker.sock.
	// If that socket doesn't exist, probe known alternative locations (e.g. Colima).
	if dockerHost == "" {
		if sock := findDockerSocket(); sock != "" {
			opts = append(opts, client.WithHost("unix://"+sock))
		}
	}

	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, err
	}
	return &DockerRuntime{client: cli}, nil
}

func findDockerSocket() string {
	// Lima VM: Docker socket is natively available at the standard path.
	// Lima sets LIMA_INSTANCE inside the VM.
	if os.Getenv("LIMA_INSTANCE") != "" {
		return "/var/run/docker.sock"
	}

	home, _ := os.UserHomeDir()
	return probeSocket(
		filepath.Join(home, ".colima", "default", "docker.sock"),
		filepath.Join(home, ".colima", "docker.sock"),
		filepath.Join(home, ".orbstack", "run", "docker.sock"),
		filepath.Join(home, ".lima", "docker", "sock", "docker.sock"),
	)
}

func probeSocket(candidates ...string) string {
	for _, sock := range candidates {
		if _, err := os.Stat(sock); err == nil {
			return sock
		}
	}
	return ""
}

func (d *DockerRuntime) SocketPath() string {
	return socketPathFromHost(d.client.DaemonHost())
}

// Resolves the host-side Docker socket path to bind-mount into containers.
// On Unix, it strips the unix:// prefix. On Windows, Docker Desktop connects via a named pipe
// but exposes the socket at /var/run/docker.sock for Linux containers to bind-mount (via WSL2).
func socketPathFromHost(host string) string {
	if strings.HasPrefix(host, "unix://") {
		return strings.TrimPrefix(host, "unix://")
	}
	if strings.HasPrefix(host, "npipe://") {
		return "/var/run/docker.sock"
	}
	return ""
}

func (d *DockerRuntime) IsHealthy(ctx context.Context) error {
	_, err := d.client.Ping(ctx)
	if err != nil {
		return fmt.Errorf("cannot connect to Docker daemon: %w", err)
	}
	return nil
}

func (d *DockerRuntime) EmitUnhealthyError(sink output.Sink, err error) {
	actions := []output.ErrorAction{
		{Label: "Install Docker:", Value: "https://docs.docker.com/get-docker/"},
	}
	summary := err.Error()
	switch stdruntime.GOOS {
	case "darwin":
		actions = append([]output.ErrorAction{{Label: "Start Docker Desktop:", Value: "open -a Docker"}}, actions...)
	case "linux":
		actions = append([]output.ErrorAction{{Label: "Start Docker:", Value: "sudo systemctl start docker"}}, actions...)
	case "windows":
		actions = append([]output.ErrorAction{{Label: "Start Docker Desktop:", Value: windowsDockerStartCommand(os.Getenv, exec.LookPath)}}, actions...)
		// Suppress the raw error: on Windows it's a named-pipe message that users can't act on.
		summary = ""
	}
	output.EmitError(sink, output.ErrorEvent{
		Title:   "Docker is not available",
		Summary: summary,
		Actions: actions,
	})
}

// PSModulePath is always set by PowerShell and never by cmd.exe; use it to pick the right start command.
// Prefers "docker desktop start" (documented CLI method); falls back to the full executable path.
func windowsDockerStartCommand(getenv func(string) string, lookPath func(string) (string, error)) string {
	if _, err := lookPath("docker"); err == nil {
		return "docker desktop start"
	}
	const exePath = `C:\Program Files\Docker\Docker\Docker Desktop.exe`
	if getenv("PSModulePath") != "" {
		return "& '" + exePath + "'"
	}
	return `"` + exePath + `"`
}

func (d *DockerRuntime) PullImage(ctx context.Context, imageName string, progress chan<- PullProgress) error {
	reader, err := d.client.ImagePull(ctx, imageName, image.PullOptions{})
	if err != nil {
		return err
	}
	defer func() {
		if err := reader.Close(); err != nil {
			log.Printf("failed to close image pull reader: %v", err)
		}
	}()

	if progress != nil {
		defer close(progress)
	}

	decoder := json.NewDecoder(reader)
	for {
		var msg struct {
			Status         string `json:"status"`
			ID             string `json:"id"`
			Error          string `json:"error"`
			ProgressDetail struct {
				Current int64 `json:"current"`
				Total   int64 `json:"total"`
			} `json:"progressDetail"`
		}
		if err := decoder.Decode(&msg); err == io.EOF {
			break
		} else if err != nil {
			return err
		}

		if msg.Error != "" {
			return fmt.Errorf("image pull failed: %s", msg.Error)
		}

		if progress != nil {
			progress <- PullProgress{
				LayerID: msg.ID,
				Status:  msg.Status,
				Current: msg.ProgressDetail.Current,
				Total:   msg.ProgressDetail.Total,
			}
		}
	}
	return nil
}

func (d *DockerRuntime) Start(ctx context.Context, config ContainerConfig) (string, error) {
	containerPort := nat.Port(config.ContainerPort)
	exposedPorts := nat.PortSet{containerPort: struct{}{}}
	portBindings := nat.PortMap{containerPort: []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: config.Port}}}

	for _, ep := range config.ExtraPorts {
		proto := ep.Protocol
		if proto == "" {
			proto = "tcp"
		}
		p := nat.Port(ep.ContainerPort + "/" + proto)
		exposedPorts[p] = struct{}{}
		portBindings[p] = []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: ep.HostPort}}
	}

	var binds []string
	for _, b := range config.Binds {
		bind := b.HostPath + ":" + b.ContainerPath
		if b.ReadOnly {
			bind += ":ro"
		}
		binds = append(binds, bind)
	}

	resp, err := d.client.ContainerCreate(ctx,
		&container.Config{
			Image:        config.Image,
			ExposedPorts: exposedPorts,
			Env:          config.Env,
		},
		&container.HostConfig{
			PortBindings: portBindings,
			Binds:        binds,
		},
		nil, nil, config.Name,
	)
	if err != nil {
		return "", err
	}

	if err := d.client.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return "", err
	}

	return resp.ID, nil
}

func (d *DockerRuntime) Stop(ctx context.Context, containerName string) error {
	if err := d.client.ContainerStop(ctx, containerName, container.StopOptions{}); err != nil {
		return err
	}
	return d.client.ContainerRemove(ctx, containerName, container.RemoveOptions{})
}

func (d *DockerRuntime) Remove(ctx context.Context, containerName string) error {
	return d.client.ContainerRemove(ctx, containerName, container.RemoveOptions{})
}

func (d *DockerRuntime) IsRunning(ctx context.Context, containerID string) (bool, error) {
	inspect, err := d.client.ContainerInspect(ctx, containerID)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return inspect.State.Running, nil
}

func (d *DockerRuntime) ContainerStartedAt(ctx context.Context, containerName string) (time.Time, error) {
	inspect, err := d.client.ContainerInspect(ctx, containerName)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to inspect container: %w", err)
	}
	t, err := time.Parse(time.RFC3339Nano, inspect.State.StartedAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to parse container start time: %w", err)
	}
	return t, nil
}

func (d *DockerRuntime) Logs(ctx context.Context, containerID string, tail int) (string, error) {
	options := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       "50",
	}
	if tail > 0 {
		options.Tail = strconv.Itoa(tail)
	}

	reader, err := d.client.ContainerLogs(ctx, containerID, options)
	if err != nil {
		return "", err
	}
	defer func() {
		if err := reader.Close(); err != nil {
			log.Printf("failed to close logs reader: %v", err)
		}
	}()

	logs, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}

	return string(logs), nil
}

func (d *DockerRuntime) StreamLogs(ctx context.Context, containerID string, out io.Writer, follow bool) error {
	reader, err := d.client.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     follow,
		Tail:       "all",
	})
	if err != nil {
		if errdefs.IsNotFound(err) {
			return fmt.Errorf("emulator is not running. Start LocalStack with `lstk`")
		}
		return fmt.Errorf("failed to stream logs for %s: %w", containerID, err)
	}
	defer func() {
		if err := reader.Close(); err != nil {
			log.Printf("failed to close logs reader: %v", err)
		}
	}()

	// Docker combines stdout and stderr into one stream, prefixing each chunk with
	// an 8-byte header that identifies which stream it belongs to. StdCopy reads
	// those headers and routes each chunk to the correct writer.
	_, err = stdcopy.StdCopy(out, out, reader)
	if err != nil && ctx.Err() == nil {
		return fmt.Errorf("error reading logs: %w", err)
	}
	return nil
}

func (d *DockerRuntime) GetBoundPort(ctx context.Context, containerName string, containerPort string) (string, error) {
	inspect, err := d.client.ContainerInspect(ctx, containerName)
	if err != nil {
		return "", fmt.Errorf("failed to inspect container: %w", err)
	}
	bindings, ok := inspect.NetworkSettings.Ports[nat.Port(containerPort)]
	if !ok || len(bindings) == 0 {
		return "", fmt.Errorf("no binding found for port %s on container %s", containerPort, containerName)
	}
	return bindings[0].HostPort, nil
}

func (d *DockerRuntime) GetImageVersion(ctx context.Context, imageName string) (string, error) {
	inspect, err := d.client.ImageInspect(ctx, imageName)
	if err != nil {
		return "", fmt.Errorf("failed to inspect image: %w", err)
	}

	// Get version from LOCALSTACK_BUILD_VERSION environment variable
	if inspect.Config != nil && inspect.Config.Env != nil {
		for _, env := range inspect.Config.Env {
			if strings.HasPrefix(env, "LOCALSTACK_BUILD_VERSION=") {
				return strings.TrimPrefix(env, "LOCALSTACK_BUILD_VERSION="), nil
			}
		}
	}

	return "", fmt.Errorf("LOCALSTACK_BUILD_VERSION not found in image environment")
}
