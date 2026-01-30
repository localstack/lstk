package runtime

import (
	"context"
	"encoding/json"
	"io"
	"strconv"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
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
	defer reader.Close()

	if progress != nil {
		defer close(progress)
	}

	decoder := json.NewDecoder(reader)
	for {
		var msg struct {
			Status         string `json:"status"`
			ID             string `json:"id"`
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
	defer reader.Close()

	logs, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}

	return string(logs), nil
}
