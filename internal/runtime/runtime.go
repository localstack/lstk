package runtime

import "context"

type ContainerConfig struct {
	Image      string
	Name       string
	Port       string
	HealthPath string
	Env        []string // e.g., ["KEY=value", "FOO=bar"]
}

type PullProgress struct {
	LayerID string
	Status  string
	Current int64
	Total   int64
}

// Runtime abstracts container runtime operations (Docker, Podman, Kubernetes, etc.)
type Runtime interface {
	PullImage(ctx context.Context, image string, progress chan<- PullProgress) error
	Start(ctx context.Context, config ContainerConfig) (string, error)
	Stop(ctx context.Context, containerName string) error
	Remove(ctx context.Context, containerName string) error
	IsRunning(ctx context.Context, containerID string) (bool, error)
	Logs(ctx context.Context, containerID string, tail int) (string, error)
}
