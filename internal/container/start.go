package container

import (
	"context"
	"fmt"
	"net/http"
	"os"
	stdruntime "runtime"
	"slices"
	"time"

	"github.com/containerd/errdefs"
	"github.com/localstack/lstk/internal/api"
	"github.com/localstack/lstk/internal/auth"
	"github.com/localstack/lstk/internal/awsconfig"
	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/endpoint"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/ports"
	"github.com/localstack/lstk/internal/runtime"
)

type postStartSetupFunc func(ctx context.Context, sink output.Sink, interactive bool, resolvedHost string) error

// StartOptions groups the user-provided options for starting an emulator.
type StartOptions struct {
	PlatformClient   api.PlatformAPI
	AuthToken        string
	ForceFileKeyring bool
	WebAppURL        string
	LocalStackHost   string
	Containers       []config.ContainerConfig
	Env              map[string]map[string]string
}

func Start(ctx context.Context, rt runtime.Runtime, sink output.Sink, opts StartOptions, interactive bool) error {
	if err := rt.IsHealthy(ctx); err != nil {
		rt.EmitUnhealthyError(sink, err)
		return output.NewSilentError(fmt.Errorf("runtime not healthy: %w", err))
	}

	tokenStorage, err := auth.NewTokenStorage(opts.ForceFileKeyring)
	if err != nil {
		return fmt.Errorf("failed to initialize token storage: %w", err)
	}
	a := auth.New(sink, opts.PlatformClient, tokenStorage, opts.AuthToken, opts.WebAppURL, interactive)

	token, err := a.GetToken(ctx)
	if err != nil {
		return err
	}

	if hasDuplicateContainerTypes(opts.Containers) {
		output.EmitWarning(sink, "Multiple emulators of the same type are defined in your config; this setup is not supported yet")
	}

	containers := make([]runtime.ContainerConfig, len(opts.Containers))
	for i, c := range opts.Containers {
		image, err := c.Image()
		if err != nil {
			return err
		}
		healthPath, err := c.HealthPath()
		if err != nil {
			return err
		}
		productName, err := c.ProductName()
		if err != nil {
			return err
		}

		resolvedEnv, err := c.ResolvedEnv(opts.Env)
		if err != nil {
			return err
		}
		env := append(resolvedEnv, "LOCALSTACK_AUTH_TOKEN="+token)
		containers[i] = runtime.ContainerConfig{
			Image:       image,
			Name:        c.Name(),
			Port:        c.Port,
			HealthPath:  healthPath,
			Env:         env,
			Tag:         c.Tag,
			ProductName: productName,
		}
	}

	containers, err = selectContainersToStart(ctx, rt, sink, containers)
	if err != nil {
		return err
	}
	if len(containers) == 0 {
		return nil
	}

	// TODO validate license for tag "latest" without resolving the actual image version,
	// and avoid pulling all images first
	if err := pullImages(ctx, rt, sink, containers); err != nil {
		return err
	}

	if err := validateLicenses(ctx, rt, sink, opts.PlatformClient, containers, token); err != nil {
		return err
	}

	if err := startContainers(ctx, rt, sink, containers); err != nil {
		return err
	}

	// Maps emulator types to their post-start setup functions.
	// Add an entry here to run setup for a new emulator type (e.g. Azure, Snowflake).
	setups := map[config.EmulatorType]postStartSetupFunc{
		config.EmulatorAWS: awsconfig.Setup,
	}
	return runPostStartSetups(ctx, sink, opts.Containers, interactive, opts.LocalStackHost, setups)
}

func runPostStartSetups(ctx context.Context, sink output.Sink, containers []config.ContainerConfig, interactive bool, localStackHost string, setups map[config.EmulatorType]postStartSetupFunc) error {
	// build ordered list of unique types, keeping the first container config for each
	firstByType := map[config.EmulatorType]config.ContainerConfig{}
	var uniqueEmulatorTypes []config.EmulatorType
	for _, c := range containers {
		if !slices.Contains(uniqueEmulatorTypes, c.Type) {
			uniqueEmulatorTypes = append(uniqueEmulatorTypes, c.Type)
			firstByType[c.Type] = c
		}
	}
	for _, t := range uniqueEmulatorTypes {
		if setup, ok := setups[t]; ok {
			resolvedHost, dnsOK := endpoint.ResolveHost(firstByType[t].Port, localStackHost)
			if !dnsOK {
				output.EmitNote(sink, `Could not resolve "localhost.localstack.cloud" — your system may have DNS rebind protection enabled. Using 127.0.0.1 as the endpoint.`)
			}
			if err := setup(ctx, sink, interactive, resolvedHost); err != nil {
				return err
			}
		}
	}
	return nil
}

func pullImages(ctx context.Context, rt runtime.Runtime, sink output.Sink, containers []runtime.ContainerConfig) error {
	for _, c := range containers {
		// Remove any existing stopped container with the same name
		if err := rt.Remove(ctx, c.Name); err != nil && !errdefs.IsNotFound(err) {
			return fmt.Errorf("failed to remove existing container %s: %w", c.Name, err)
		}

		output.EmitSpinnerStart(sink, fmt.Sprintf("Pulling %s", c.Image))
		output.EmitStatus(sink, "pulling", c.Image, "")
		progress := make(chan runtime.PullProgress)
		go func() {
			for p := range progress {
				output.EmitProgress(sink, c.Image, p.LayerID, p.Status, p.Current, p.Total)
			}
		}()
		if err := rt.PullImage(ctx, c.Image, progress); err != nil {
			output.EmitSpinnerStop(sink)
			output.EmitError(sink, output.ErrorEvent{
				Title:   fmt.Sprintf("Failed to pull %s", c.Image),
				Summary: err.Error(),
			})
			return output.NewSilentError(fmt.Errorf("failed to pull image %s: %w", c.Image, err))
		}
		output.EmitSpinnerStop(sink)
		output.EmitSuccess(sink, fmt.Sprintf("Pulled %s", c.Image))
	}
	return nil
}

func validateLicenses(ctx context.Context, rt runtime.Runtime, sink output.Sink, platformClient api.PlatformAPI, containers []runtime.ContainerConfig, token string) error {
	for _, c := range containers {
		if err := validateLicense(ctx, rt, sink, platformClient, c, token); err != nil {
			return err
		}
	}
	return nil
}

func startContainers(ctx context.Context, rt runtime.Runtime, sink output.Sink, containers []runtime.ContainerConfig) error {
	for _, c := range containers {
		output.EmitStatus(sink, "starting", c.Name, "")
		containerID, err := rt.Start(ctx, c)
		if err != nil {
			return fmt.Errorf("failed to start LocalStack: %w", err)
		}

		output.EmitStatus(sink, "waiting", c.Name, "")
		healthURL := fmt.Sprintf("http://localhost:%s%s", c.Port, c.HealthPath)
		if err := awaitStartup(ctx, rt, sink, containerID, "LocalStack", healthURL); err != nil {
			return err
		}

		output.EmitStatus(sink, "ready", c.Name, fmt.Sprintf("containerId: %s", containerID[:12]))
	}
	return nil
}

func selectContainersToStart(ctx context.Context, rt runtime.Runtime, sink output.Sink, containers []runtime.ContainerConfig) ([]runtime.ContainerConfig, error) {
	var filtered []runtime.ContainerConfig
	for _, c := range containers {
		running, err := rt.IsRunning(ctx, c.Name)
		if err != nil && !errdefs.IsNotFound(err) {
			return nil, fmt.Errorf("failed to check container status: %w", err)
		}
		if running {
			output.EmitInfo(sink, "LocalStack is already running")
			continue
		}
		if err := ports.CheckAvailable(c.Port); err != nil {
			emitPortInUseError(sink, c.Port)
			return nil, output.NewSilentError(err)
		}
		filtered = append(filtered, c)
	}
	return filtered, nil
}

func emitPortInUseError(sink output.Sink, port string) {
	actions := []output.ErrorAction{
		{Label: "Stop existing emulator:", Value: "lstk stop"},
	}
	configPath, pathErr := config.ConfigFilePath()
	if pathErr == nil {
		actions = append(actions, output.ErrorAction{Label: "Use another port in the configuration:", Value: configPath})
	}
	output.EmitError(sink, output.ErrorEvent{
		Title:   fmt.Sprintf("Port %s already in use", port),
		Summary: "LocalStack may already be running.",
		Actions: actions,
	})
}

func validateLicense(ctx context.Context, rt runtime.Runtime, sink output.Sink, platformClient api.PlatformAPI, containerConfig runtime.ContainerConfig, token string) error {
	version := containerConfig.Tag
	if version == "" || version == "latest" {
		actualVersion, err := rt.GetImageVersion(ctx, containerConfig.Image)
		if err != nil {
			return fmt.Errorf("could not resolve version from image %s: %w", containerConfig.Image, err)
		}
		version = actualVersion
	}

	output.EmitStatus(sink, "validating license", containerConfig.Name, version)

	hostname, _ := os.Hostname()
	licenseReq := &api.LicenseRequest{
		Product: api.ProductInfo{
			Name:    containerConfig.ProductName,
			Version: version,
		},
		Credentials: api.CredentialsInfo{
			Token: token,
		},
		Machine: api.MachineInfo{
			Hostname:        hostname,
			Platform:        stdruntime.GOOS,
			PlatformRelease: stdruntime.GOARCH,
		},
	}

	if err := platformClient.GetLicense(ctx, licenseReq); err != nil {
		return fmt.Errorf("license validation failed for %s:%s: %w", containerConfig.ProductName, version, err)
	}

	return nil
}

// awaitStartup polls until one of two outcomes:
//   - Success: health endpoint returns 200 (license is valid, LocalStack is ready)
//   - Failure: container stops running (e.g., license activation failed), returns error with container logs
//
// TODO: move to Runtime interface if other runtimes (k8s?) need native readiness probes
func awaitStartup(ctx context.Context, rt runtime.Runtime, sink output.Sink, containerID, name, healthURL string) error {
	client := &http.Client{Timeout: 2 * time.Second}

	for {
		running, err := rt.IsRunning(ctx, containerID)
		if err != nil {
			return fmt.Errorf("failed to check container status: %w", err)
		}
		if !running {
			logs, logsErr := rt.Logs(ctx, containerID, 20)
			if logsErr != nil || logs == "" {
				return fmt.Errorf("%s exited unexpectedly", name)
			}
			return fmt.Errorf("%s exited unexpectedly:\n%s", name, logs)
		}

		resp, err := client.Get(healthURL)
		if err == nil && resp.StatusCode == http.StatusOK {
			if err := resp.Body.Close(); err != nil {
				output.EmitWarning(sink, fmt.Sprintf("failed to close response body: %v", err))
			}
			return nil
		}
		if resp != nil {
			if err := resp.Body.Close(); err != nil {
				output.EmitWarning(sink, fmt.Sprintf("failed to close response body: %v", err))
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1 * time.Second):
		}
	}
}

func hasDuplicateContainerTypes(containers []config.ContainerConfig) bool {
	seen := make(map[config.EmulatorType]bool)
	for _, c := range containers {
		if seen[c.Type] {
			return true
		}
		seen[c.Type] = true
	}
	return false
}
