package runtime

import (
	"context"
	"io"
	"time"

	"github.com/localstack/lstk/internal/output"
)

type ContainerConfig struct {
	Image       string
	Name        string
	Port        string
	HealthPath  string
	Env         []string // e.g., ["KEY=value", "FOO=bar"]
	Tag         string
	ProductName string
}

type PullProgress struct {
	LayerID string
	Status  string
	Current int64
	Total   int64
}

// Runtime abstracts container runtime operations (Docker, Podman, Kubernetes, etc.)
type Runtime interface {
	IsHealthy(ctx context.Context) error
	EmitUnhealthyError(sink output.Sink, err error)
	PullImage(ctx context.Context, image string, progress chan<- PullProgress) error
	Start(ctx context.Context, config ContainerConfig) (string, error)
	Stop(ctx context.Context, containerName string) error
	Remove(ctx context.Context, containerName string) error
	IsRunning(ctx context.Context, containerID string) (bool, error)
	ContainerStartedAt(ctx context.Context, containerName string) (time.Time, error)
	Logs(ctx context.Context, containerID string, tail int) (string, error)
	StreamLogs(ctx context.Context, containerID string, out io.Writer, follow bool) error
	GetImageVersion(ctx context.Context, imageName string) (string, error)
}
