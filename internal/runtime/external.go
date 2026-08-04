package runtime

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/localstack/lstk/internal/output"
)

// ErrExternalRuntimeUnsupported is returned by externalRuntime methods that
// have no meaning for an emulator lstk did not start and does not manage.
var ErrExternalRuntimeUnsupported = errors.New("not supported for an externally-managed emulator (--endpoint-url)")

// externalRuntime is a Runtime standing in for an emulator resolved via
// --endpoint-url/LSTK_ENDPOINT_URL/AWS_ENDPOINT_URL rather than discovered via
// Docker. It reports itself healthy and reports the single container name it
// was constructed for as running, so callers built around container-name
// discovery (container.RunningEmulators, container.ResolveRunningContainerName)
// work unchanged against an externally-managed endpoint with no Docker
// involvement — the reachability/type probe that resolved the endpoint
// already stands in for these checks. Every other Runtime method is a Docker
// lifecycle operation with no meaning here and returns
// ErrExternalRuntimeUnsupported.
type externalRuntime struct {
	containerName string
}

// NewExternalRuntime returns a Runtime standing in for an externally-managed
// emulator, matching the container name of the config.ContainerConfig the
// caller constructs for it (so container.ResolveRunningContainerName's
// rt.IsRunning(ctx, c.Name()) check finds it "running"). See externalRuntime.
func NewExternalRuntime(containerName string) Runtime {
	return &externalRuntime{containerName: containerName}
}

func (r *externalRuntime) IsHealthy(context.Context) error { return nil }

// EmitUnhealthyError is never called: IsHealthy never returns an error.
func (r *externalRuntime) EmitUnhealthyError(output.Sink, error) {}

func (r *externalRuntime) IsRunning(_ context.Context, containerID string) (bool, error) {
	return containerID == r.containerName, nil
}

// FindRunningByImage is never reached in practice: IsRunning(r.containerName)
// always matches first in container.ResolveRunningContainerName.
func (r *externalRuntime) FindRunningByImage(context.Context, []string, string) (*RunningContainer, error) {
	return nil, nil
}

func (r *externalRuntime) PullImage(context.Context, string, chan<- PullProgress) error {
	return ErrExternalRuntimeUnsupported
}

func (r *externalRuntime) Start(context.Context, ContainerConfig) (string, <-chan ExitResult, error) {
	return "", nil, ErrExternalRuntimeUnsupported
}

func (r *externalRuntime) Stop(context.Context, string) error   { return ErrExternalRuntimeUnsupported }
func (r *externalRuntime) Remove(context.Context, string) error { return ErrExternalRuntimeUnsupported }

func (r *externalRuntime) InspectBrief(context.Context, string) (ContainerBrief, error) {
	return ContainerBrief{}, ErrExternalRuntimeUnsupported
}

func (r *externalRuntime) ContainerStartedAt(context.Context, string) (time.Time, error) {
	return time.Time{}, ErrExternalRuntimeUnsupported
}

func (r *externalRuntime) ContainerEnv(context.Context, string) ([]string, error) {
	return nil, ErrExternalRuntimeUnsupported
}

func (r *externalRuntime) Logs(context.Context, string, int) (string, error) {
	return "", ErrExternalRuntimeUnsupported
}

func (r *externalRuntime) StreamLogs(context.Context, string, io.Writer, bool, string) error {
	return ErrExternalRuntimeUnsupported
}

func (r *externalRuntime) GetImageVersion(context.Context, string) (string, error) {
	return "", ErrExternalRuntimeUnsupported
}

func (r *externalRuntime) ImageExists(context.Context, string) (bool, error) {
	return false, ErrExternalRuntimeUnsupported
}

func (r *externalRuntime) GetBoundPort(context.Context, string, string) (string, error) {
	return "", ErrExternalRuntimeUnsupported
}

func (r *externalRuntime) SocketPath() string { return "" }

// Flavor reports FlavorUnknown: there is no local daemon connection behind an
// externally-managed endpoint to identify.
func (r *externalRuntime) Flavor() string { return FlavorUnknown }
