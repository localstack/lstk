package runtime

import "context"

type ContainerConfig struct {
	Image string
	Name  string
	Ports map[string]string
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
	IsRunning(ctx context.Context, containerID string) (bool, error)
}
