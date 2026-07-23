package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	stdruntime "runtime"
	"strconv"
	"strings"
	"time"

	"net/netip"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/localstack/lstk/internal/output"
)

type DockerRuntime struct {
	client *client.Client
}

func NewDockerRuntime(dockerHost string) (*DockerRuntime, error) {
	opts := []client.Opt{
		client.FromEnv,
		client.WithTraceOptions(
			otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
				return "docker " + r.Method + " " + r.URL.Path
			}),
		),
	}

	// When DOCKER_HOST is not set, prefer the Docker CLI's own notion of where the
	// daemon lives (its current context, which Rancher Desktop and OrbStack both
	// register) since that self-maintains as new runtimes are installed. Fall back
	// to probing known alternative socket locations (e.g. Colima, Podman), then to
	// the Docker SDK's default (/var/run/docker.sock).
	if dockerHost == "" {
		if host := resolveDockerContextHost(os.Getenv, defaultDockerConfigDir(os.Getenv)); host != "" {
			opts = append(opts, client.WithHost(host))
		} else if sock := findDockerSocket(); sock != "" {
			opts = append(opts, client.WithHost("unix://"+sock))
		}
	}

	cli, err := client.New(opts...)
	if err != nil {
		return nil, err
	}
	return &DockerRuntime{client: cli}, nil
}

// vmSocketPaths lists user-scoped sockets for VM-backed runtimes: the daemon runs
// inside a VM and the socket seen here is a remapped/forwarded view of it, so
// isVM() treats a match as needing the /var/run/docker.sock rewrite for container
// bind-mounts (see SocketPath). Podman's macOS "machine" backend is VM-based too and
// exposes a Docker-compatible socket, hence its inclusion here rather than in
// nativeSocketPaths.
func vmSocketPaths(home string) []string {
	paths := make([]string, len(vmSocketSpecs))
	for i, spec := range vmSocketSpecs {
		paths[i] = filepath.Join(home, spec.relPath)
	}
	return paths
}

// nativeSocketSpec pairs one nativeSocketPaths() entry with the runtime flavor
// (rootful vs. rootless Podman) it belongs to, mirroring how vmSocketSpec pairs
// vmSocketPaths() entries with their flavor.
type nativeSocketSpec struct {
	flavor runtimeFlavor
	path   string
}

// nativeSocketSpecs lists sockets for daemons that run directly on the host (no VM
// layer), so unlike vmSocketPaths their path is also the daemon-visible path used
// for the Lambda container bind-mount — isVM() must not match these. Rootful and
// rootless Podman are tracked as distinct flavors since they're started with
// different systemctl invocations (see tailoredRuntimeAction).
func nativeSocketSpecs() []nativeSocketSpec {
	specs := []nativeSocketSpec{
		{flavorPodmanRootful, filepath.Join("/run", "podman", "podman.sock")},
	}
	if xdgRuntimeDir := os.Getenv("XDG_RUNTIME_DIR"); xdgRuntimeDir != "" {
		specs = append(specs, nativeSocketSpec{flavorPodmanRootless, filepath.Join(xdgRuntimeDir, "podman", "podman.sock")})
	}
	return specs
}

// nativeSocketPaths returns just the paths from nativeSocketSpecs, for callers
// (e.g. probeSocket) that only care about socket discovery, not flavor.
func nativeSocketPaths() []string {
	specs := nativeSocketSpecs()
	paths := make([]string, len(specs))
	for i, spec := range specs {
		paths[i] = spec.path
	}
	return paths
}

func findDockerSocket() string {
	// Lima sets LIMA_INSTANCE inside the VM; the socket is at the standard path natively.
	if os.Getenv("LIMA_INSTANCE") != "" {
		return "/var/run/docker.sock"
	}

	home, _ := os.UserHomeDir()
	if sock := probeSocket(vmSocketPaths(home)...); sock != "" {
		return sock
	}
	return probeSocket(nativeSocketPaths()...)
}

// Dial as well as stat: a socket file may linger after its daemon is gone (e.g. a
// stale ~/.docker/run/docker.sock from an uninstalled Docker Desktop) and would
// otherwise shadow a live socket later in the list.
func probeSocket(candidates ...string) string {
	for _, sock := range candidates {
		if _, err := os.Stat(sock); err != nil {
			continue
		}
		conn, err := net.DialTimeout("unix", sock, 200*time.Millisecond)
		if err != nil {
			continue
		}
		_ = conn.Close()
		return sock
	}
	return ""
}

// isVM reports whether the Docker daemon is running inside a VM (e.g., Docker
// Desktop, Colima, OrbStack, Lima, Rancher Desktop, Podman machine on macOS).
// In these cases the socket is remapped inside the VM and the container sees it at
// /var/run/docker.sock even if the CLI connects via a user-scoped socket path. This
// holds regardless of how that path was resolved (context lookup or probing), since
// isVM checks the daemon host actually in use — native (non-VM) runtimes such as
// Linux Podman are deliberately kept out of vmSocketPaths so they're never matched.
func (d *DockerRuntime) isVM() bool {
	host := d.client.DaemonHost()
	if strings.HasPrefix(host, "unix://") {
		socketPath := strings.TrimPrefix(host, "unix://")
		home, _ := os.UserHomeDir()
		for _, vmSock := range vmSocketPaths(home) {
			if socketPath == vmSock {
				return true
			}
		}
	}
	return false
}

// SocketPath returns the daemon-visible Unix socket path to bind-mount into
// containers so LocalStack can launch nested workloads such as Lambda functions.
// For VM-based Docker (Colima, OrbStack) returns /var/run/docker.sock as the
// socket is remapped inside the VM. For rootless or custom setups, returns the
// actual socket path extracted from the daemon host.
func (d *DockerRuntime) Flavor() string {
	home, _ := os.UserHomeDir()
	return classifySocketFlavor(home, d.client.DaemonHost()).String()
}

func (d *DockerRuntime) SocketPath() string {
	if d.isVM() {
		return "/var/run/docker.sock"
	}
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
	_, err := d.client.Ping(ctx, client.PingOptions{})
	if err != nil {
		return fmt.Errorf("cannot connect to Docker daemon: %w", err)
	}
	return nil
}

func (d *DockerRuntime) EmitUnhealthyError(sink output.Sink, err error) {
	home, _ := os.UserHomeDir()
	d.emitUnhealthyError(sink, err, home, os.Stat, exec.LookPath, os.Getenv, stdruntime.GOOS)
}

// emitUnhealthyError is EmitUnhealthyError with its runtime-detection evidence
// (home dir, stat/lookPath/getenv, GOOS) injected, following the same style as
// windowsDockerStartCommand so tests can simulate any runtime being installed
// without touching the real filesystem or PATH.
func (d *DockerRuntime) emitUnhealthyError(
	sink output.Sink,
	err error,
	home string,
	statFn func(string) (os.FileInfo, error),
	lookPath func(string) (string, error),
	getenv func(string) string,
	goos string,
) {
	actions := []output.ErrorAction{
		{Label: "Install Docker:", Value: "https://docs.docker.com/get-docker/"},
	}
	summary := err.Error()
	switch goos {
	case "darwin":
		actions = append([]output.ErrorAction{{Label: "Start Docker Desktop:", Value: "open -a Docker"}}, actions...)
	case "linux":
		actions = append([]output.ErrorAction{{Label: "Start Docker:", Value: "sudo systemctl start docker"}}, actions...)
	case "windows":
		actions = append([]output.ErrorAction{{Label: "Start Docker Desktop:", Value: windowsDockerStartCommand(getenv, lookPath)}}, actions...)
		// Suppress the raw error: on Windows it's a named-pipe message that users can't act on.
		summary = ""
	}

	// The daemon is unreachable, but the socket path (if any) the client was
	// configured with, or filesystem/PATH evidence of another runtime, can often
	// tell us what the user actually has installed. Put that ahead of the
	// generic Docker actions above so the most relevant hint reads first.
	configuredFlavor := classifySocketFlavor(home, d.client.DaemonHost())
	flavor := detectRuntimeFlavor(configuredFlavor, home, statFn, lookPath, goos)
	if tailored, ok := tailoredRuntimeAction(flavor, goos); ok {
		actions = append([]output.ErrorAction{tailored}, actions...)
	}

	sink.Emit(output.ErrorEvent{
		Title:   "Docker is not available",
		Summary: summary,
		Actions: actions,
		Code:    output.ErrRuntimeUnavailable,
	})
}

// detectRuntimeFlavor identifies the runtime most likely installed when the
// daemon can't be reached: the socket the client was actually configured with
// wins if recognized, otherwise it falls back to filesystem/PATH evidence for
// each runtime. Install-specific state (a runtime's own home-dir/state
// directory) is checked before the podman CLI's mere presence on PATH, since a
// machine can have the podman binary installed (e.g. as a Docker CLI helper)
// while actually running a different runtime — the state directory is the
// stronger signal of the two.
func detectRuntimeFlavor(
	configuredFlavor runtimeFlavor,
	home string,
	statFn func(string) (os.FileInfo, error),
	lookPath func(string) (string, error),
	goos string,
) runtimeFlavor {
	// flavorDockerNative just means the client fell back to the SDK's plain
	// default socket, not that we positively identified Docker as installed —
	// so unlike other recognized flavors it must not short-circuit the
	// filesystem/PATH evidence checks below.
	if configuredFlavor != flavorUnknown && configuredFlavor != flavorDockerNative {
		return configuredFlavor
	}
	if _, err := statFn(filepath.Join(home, ".rd")); err == nil {
		return flavorRancherDesktop
	}
	if _, err := statFn(filepath.Join(home, ".colima")); err == nil {
		return flavorColima
	}
	if _, err := statFn(filepath.Join(home, ".config", "colima")); err == nil {
		return flavorColima
	}
	if goos == "darwin" {
		if _, err := statFn(filepath.Join(home, ".orbstack")); err == nil {
			return flavorOrbstack
		}
	}
	// Weakest evidence checked last: the podman CLI merely being on PATH doesn't
	// prove Podman is the runtime actually in use (unlike the state-directory
	// checks above), and we have no socket path here to tell rootful from
	// rootless, so this can only ever resolve to the ambiguous flavorPodman.
	if _, err := lookPath("podman"); err == nil {
		return flavorPodman
	}
	return flavorUnknown
}

// tailoredRuntimeAction returns the start hint for a detected non-Docker
// runtime, or ok=false if flavor has no dedicated hint (e.g. Docker Desktop/
// native Docker are already covered by the generic per-OS actions, and Lima
// has no separate CLI start command of its own).
func tailoredRuntimeAction(flavor runtimeFlavor, goos string) (output.ErrorAction, bool) {
	switch flavor {
	case flavorRancherDesktop:
		// rdctl ships with Rancher Desktop and works cross-platform, unlike the
		// GUI-only "open -a Rancher Desktop" equivalent.
		return output.ErrorAction{Label: "Start Rancher Desktop:", Value: "rdctl start"}, true
	case flavorColima:
		return output.ErrorAction{Label: "Start Colima:", Value: "colima start"}, true
	case flavorPodman:
		switch goos {
		case "darwin":
			return output.ErrorAction{Label: "Start Podman:", Value: "podman machine start"}, true
		case "linux":
			// No socket-path evidence to tell rootful from rootless (e.g. only the
			// podman CLI was found on PATH); rootless is the more common default.
			return output.ErrorAction{Label: "Start Podman:", Value: "systemctl --user start podman.socket"}, true
		}
	case flavorPodmanRootful:
		return output.ErrorAction{Label: "Start Podman:", Value: "systemctl start podman"}, true
	case flavorPodmanRootless:
		return output.ErrorAction{Label: "Start Podman:", Value: "systemctl --user start podman.socket"}, true
	case flavorOrbstack:
		if goos == "darwin" {
			return output.ErrorAction{Label: "Start OrbStack:", Value: "open -a OrbStack"}, true
		}
	}
	return output.ErrorAction{}, false
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
	// Close progress unconditionally — even if ImagePull fails before returning a
	// reader — so callers that wait for the progress stream to drain never hang.
	if progress != nil {
		defer close(progress)
	}

	reader, err := d.client.ImagePull(ctx, imageName, client.ImagePullOptions{})
	if err != nil {
		return err
	}
	defer func() {
		if err := reader.Close(); err != nil {
			log.Printf("failed to close image pull reader: %v", err)
		}
	}()

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

func (d *DockerRuntime) Start(ctx context.Context, config ContainerConfig) (string, <-chan ExitResult, error) {
	bindHostStr := config.BindHost
	if bindHostStr == "" {
		bindHostStr = "127.0.0.1"
	}
	bindHost, err := netip.ParseAddr(bindHostStr)
	if err != nil {
		return "", nil, fmt.Errorf("invalid bind host %q: %w", bindHostStr, err)
	}

	containerPort, err := network.ParsePort(config.ContainerPort)
	if err != nil {
		return "", nil, fmt.Errorf("invalid container port %q: %w", config.ContainerPort, err)
	}
	exposedPorts := network.PortSet{containerPort: struct{}{}}
	portBindings := network.PortMap{containerPort: []network.PortBinding{{HostIP: bindHost, HostPort: config.Port}}}

	for _, ep := range config.ExtraPorts {
		proto := ep.Protocol
		if proto == "" {
			proto = "tcp"
		}
		p, err := network.ParsePort(ep.ContainerPort + "/" + proto)
		if err != nil {
			return "", nil, fmt.Errorf("invalid extra port %q: %w", ep.ContainerPort, err)
		}
		exposedPorts[p] = struct{}{}
		portBindings[p] = []network.PortBinding{{HostIP: bindHost, HostPort: ep.HostPort}}
	}

	var binds []string
	for _, b := range config.Binds {
		bind := b.HostPath + ":" + b.ContainerPath
		if b.ReadOnly {
			bind += ":ro"
		}
		binds = append(binds, bind)
	}

	resp, err := d.client.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{
			Image:        config.Image,
			ExposedPorts: exposedPorts,
			Env:          config.Env,
		},
		HostConfig: &container.HostConfig{
			PortBindings: portBindings,
			Binds:        binds,
			AutoRemove:   true,
		},
		Name: config.Name,
	})
	if err != nil {
		return "", nil, err
	}

	// Register the exit wait before starting: an instantly-exiting container is
	// auto-removed (--rm) so fast that a wait registered after start can miss it
	// and lose the exit code. "next-exit" cannot fire for a created but
	// not-yet-started container, so this never observes a stale exit.
	exitCh := d.waitForExit(ctx, resp.ID)

	if _, err := d.client.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{}); err != nil {
		return "", nil, err
	}

	return resp.ID, exitCh, nil
}

func (d *DockerRuntime) Stop(ctx context.Context, containerName string) error {
	if _, err := d.client.ContainerStop(ctx, containerName, client.ContainerStopOptions{}); err != nil {
		return err
	}
	_, err := d.client.ContainerRemove(ctx, containerName, client.ContainerRemoveOptions{})
	// Ignore conflict and not-found: container is gone, which is the goal.
	if err != nil && !errdefs.IsConflict(err) && !errdefs.IsNotFound(err) {
		return err
	}
	return nil
}

const (
	containerRemovalTimeout      = 10 * time.Second
	containerRemovalPollInterval = 100 * time.Millisecond
)

func (d *DockerRuntime) Remove(ctx context.Context, containerName string) error {
	_, err := d.client.ContainerRemove(ctx, containerName, client.ContainerRemoveOptions{})
	// With AutoRemove (--rm) Docker may already be removing the container, so
	// ContainerRemove can report it is already gone (not-found) or that removal is
	// in progress (conflict). Both mean the container is on its way out.
	if err != nil && !errdefs.IsNotFound(err) && !errdefs.IsConflict(err) {
		return err
	}
	// Wait until the container is actually gone, so a subsequent create reusing the
	// same name does not race the in-flight auto-removal ("name already in use").
	return d.waitContainerGone(ctx, containerName)
}

// waitContainerGone blocks until no container named containerName exists, the
// context is cancelled, or containerRemovalTimeout elapses.
func (d *DockerRuntime) waitContainerGone(ctx context.Context, containerName string) error {
	ctx, cancel := context.WithTimeout(ctx, containerRemovalTimeout)
	defer cancel()
	for {
		if _, err := d.client.ContainerInspect(ctx, containerName, client.ContainerInspectOptions{}); errdefs.IsNotFound(err) {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for container %s to be removed", containerName)
		case <-time.After(containerRemovalPollInterval):
		}
	}
}

func (d *DockerRuntime) IsRunning(ctx context.Context, containerID string) (bool, error) {
	inspect, err := d.client.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if err != nil {
		if errdefs.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return inspect.Container.State.Running, nil
}

// waitForExit returns a channel that receives exactly one ExitResult for the
// container's next exit. ContainerWait blocks until the server acknowledges the
// wait registration, so the wait is armed by the time this returns.
func (d *DockerRuntime) waitForExit(ctx context.Context, containerID string) <-chan ExitResult {
	wait := d.client.ContainerWait(ctx, containerID, client.ContainerWaitOptions{Condition: container.WaitConditionNextExit})

	// Buffered so the goroutine never leaks if the caller stops reading.
	out := make(chan ExitResult, 1)
	go func() {
		select {
		case resp := <-wait.Result:
			out <- ExitResult{ExitCode: int(resp.StatusCode)}
		case err := <-wait.Error:
			out <- ExitResult{ExitCode: -1, Err: err}
		case <-ctx.Done():
			out <- ExitResult{ExitCode: -1, Err: ctx.Err()}
		}
	}()
	return out
}

func (d *DockerRuntime) ContainerStartedAt(ctx context.Context, containerName string) (time.Time, error) {
	inspect, err := d.client.ContainerInspect(ctx, containerName, client.ContainerInspectOptions{})
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to inspect container: %w", err)
	}
	t, err := time.Parse(time.RFC3339Nano, inspect.Container.State.StartedAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to parse container start time: %w", err)
	}
	return t, nil
}

func (d *DockerRuntime) ContainerEnv(ctx context.Context, containerName string) ([]string, error) {
	inspect, err := d.client.ContainerInspect(ctx, containerName, client.ContainerInspectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to inspect container: %w", err)
	}
	if inspect.Container.Config == nil {
		return nil, nil
	}
	return inspect.Container.Config.Env, nil
}

func (d *DockerRuntime) Logs(ctx context.Context, containerID string, tail int) (string, error) {
	options := client.ContainerLogsOptions{
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
	reader, err := d.client.ContainerLogs(ctx, containerID, client.ContainerLogsOptions{
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
	inspect, err := d.client.ContainerInspect(ctx, containerName, client.ContainerInspectOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to inspect container: %w", err)
	}
	port, err := network.ParsePort(containerPort)
	if err != nil {
		return "", fmt.Errorf("invalid container port %q: %w", containerPort, err)
	}
	bindings, ok := inspect.Container.NetworkSettings.Ports[port]
	if !ok || len(bindings) == 0 {
		return "", fmt.Errorf("no binding found for port %s on container %s", containerPort, containerName)
	}
	return bindings[0].HostPort, nil
}

func (d *DockerRuntime) FindRunningByImage(ctx context.Context, imageRepos []string, containerPort string) (*RunningContainer, error) {
	list, err := d.client.ContainerList(ctx, client.ContainerListOptions{
		Filters: make(client.Filters).Add("status", "running"),
	})
	if err != nil {
		return nil, err
	}

	portStr, proto, found := strings.Cut(containerPort, "/")
	if !found {
		proto = "tcp"
	}
	privatePort, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return nil, fmt.Errorf("invalid container port %q: %w", containerPort, err)
	}

	for _, c := range list.Items {
		if !matchesAnyImageRepo(c.Image, imageRepos) {
			continue
		}
		for _, p := range c.Ports {
			if p.PrivatePort == uint16(privatePort) && p.Type == proto {
				name := ""
				if len(c.Names) > 0 {
					name = strings.TrimPrefix(c.Names[0], "/")
				}
				return &RunningContainer{
					Name:      name,
					Image:     c.Image,
					BoundPort: strconv.Itoa(int(p.PublicPort)),
				}, nil
			}
		}
	}
	return nil, nil
}

func matchesAnyImageRepo(image string, repos []string) bool {
	for _, repo := range repos {
		if image == repo || strings.HasPrefix(image, repo+":") {
			return true
		}
	}
	return false
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

func (d *DockerRuntime) ImageExists(ctx context.Context, image string) (bool, error) {
	if _, err := d.client.ImageInspect(ctx, image); err != nil {
		if errdefs.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to inspect image: %w", err)
	}
	return true, nil
}
