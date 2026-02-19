package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"

	"github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
)

// DockerRuntime implements Runtime using the Docker API.
type DockerRuntime struct {
	client *client.Client
}

func NewDockerRuntime() (*DockerRuntime, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	return &DockerRuntime{client: cli}, nil
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
	port := nat.Port(config.Port + "/tcp")
	exposedPorts := nat.PortSet{port: struct{}{}}
	portBindings := nat.PortMap{port: []nat.PortBinding{{HostPort: config.Port}}}

	resp, err := d.client.ContainerCreate(ctx,
		&container.Config{
			Image:        config.Image,
			ExposedPorts: exposedPorts,
			Env:          config.Env,
		},
		&container.HostConfig{
			PortBindings: portBindings,
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
		return false, err
	}
	return inspect.State.Running, nil
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

func (d *DockerRuntime) StreamLogs(ctx context.Context, containerID string, out io.Writer) error {
	reader, err := d.client.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
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
