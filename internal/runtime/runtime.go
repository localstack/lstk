package runtime

import (
	"context"
	"io"
	"time"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/output"
)

// BindMount represents a host-to-container bind mount.
type BindMount struct {
	HostPath      string
	ContainerPath string
	ReadOnly      bool
}

// PortMapping represents a container-to-host port mapping.
type PortMapping struct {
	ContainerPort string
	HostPort      string
	Protocol      string // "tcp" (default) or "udp"
}

type ContainerConfig struct {
	Image         string
	Name          string
	EmulatorType  config.EmulatorType
	Port          string
	ContainerPort string // internal port the emulator listens on inside the container (e.g. "4566/tcp")
	HealthPath    string
	Env           []string // e.g., ["KEY=value", "FOO=bar"]
	Tag           string
	ProductName   string
	Binds         []BindMount
	ExtraPorts    []PortMapping
}

type PullProgress struct {
	LayerID string
	Status  string
	Current int64
	Total   int64
}

type RunningContainer struct {
	Name      string
	Image     string // full image with tag, e.g. "localstack/localstack-pro:3.5.0"
	BoundPort string // host port bound to the queried container port
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
	// GetBoundPort returns the host port bound to the given container port (e.g. "4566/tcp").
	GetBoundPort(ctx context.Context, containerName string, containerPort string) (string, error)
	FindRunningByImage(ctx context.Context, imageRepos []string, containerPort string) (*RunningContainer, error)
	SocketPath() string
}
