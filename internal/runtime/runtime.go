package runtime

//go:generate mockgen -source=runtime.go -destination=mock_runtime.go -package=runtime

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
	// Optional marks a best-effort publication: when the host port is already
	// taken, the mapping is dropped with a warning instead of failing the start.
	// Used for ports lstk adds on its own (e.g. 443 from the default
	// GATEWAY_LISTEN) — ports the user asked for explicitly are never optional.
	Optional bool
}

type ContainerConfig struct {
	Image         string
	Name          string
	EmulatorType  config.EmulatorType
	Port          string
	ContainerPort string // internal port the emulator listens on inside the container (e.g. "4566/tcp")
	BindHost      string // host IP to bind published ports to (e.g. "127.0.0.1" or "0.0.0.0"); defaults to loopback when empty
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

// ContainerBrief describes an existing container for pre-start checks, so
// callers can tell an lstk leftover (safe to self-heal) from a foreign
// container that happens to use the same name.
type ContainerBrief struct {
	Exists     bool
	Running    bool
	Created    bool   // state "created": created but never started
	AutoRemove bool   // created with --rm: removes itself once it exits
	Image      string // full image the container was created from
	Managed    bool   // carries the label Start stamps on every lstk container
}

// ExitResult reports a container's exit as observed by the exit wait that
// Start registers.
type ExitResult struct {
	ExitCode int   // -1 when unknown
	Err      error // wait itself failed (exit code unknown)
}

// Runtime abstracts container runtime operations (Docker, Podman, Kubernetes, etc.)
type Runtime interface {
	IsHealthy(ctx context.Context) error
	EmitUnhealthyError(sink output.Sink, err error)
	PullImage(ctx context.Context, image string, progress chan<- PullProgress) error
	// Start creates and starts the container. The returned channel receives
	// exactly one ExitResult when the container exits. The exit wait is
	// registered between create and start so that even an instantly-exiting
	// container's exit code is observed — with AutoRemove the container is
	// removed the moment it exits, after which a wait can no longer be
	// registered.
	Start(ctx context.Context, config ContainerConfig) (string, <-chan ExitResult, error)
	Stop(ctx context.Context, containerName string) error
	Remove(ctx context.Context, containerName string) error
	IsRunning(ctx context.Context, containerID string) (bool, error)
	// InspectBrief reports whether a container with the given name exists and
	// the facts pre-start checks need about it. A missing container is
	// (ContainerBrief{}, nil), not an error.
	InspectBrief(ctx context.Context, containerName string) (ContainerBrief, error)
	ContainerStartedAt(ctx context.Context, containerName string) (time.Time, error)
	ContainerEnv(ctx context.Context, containerName string) ([]string, error)
	Logs(ctx context.Context, containerID string, tail int) (string, error)
	StreamLogs(ctx context.Context, containerID string, out io.Writer, follow bool, tail string) error
	GetImageVersion(ctx context.Context, imageName string) (string, error)
	// ImageExists reports whether the given image is already present locally.
	ImageExists(ctx context.Context, image string) (bool, error)
	// GetBoundPort returns the host port bound to the given container port (e.g. "4566/tcp").
	GetBoundPort(ctx context.Context, containerName string, containerPort string) (string, error)
	FindRunningByImage(ctx context.Context, imageRepos []string, containerPort string) (*RunningContainer, error)
	SocketPath() string
	// Flavor identifies the runtime backing the daemon connection (one of the
	// Flavor* constants, e.g. FlavorRancherDesktop) so callers can tailor
	// user-facing messages. FlavorUnknown (empty) when unrecognized.
	Flavor() string
}
