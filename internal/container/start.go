package container

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	stdruntime "runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/containerd/errdefs"
	"github.com/localstack/lstk/internal/api"
	"github.com/localstack/lstk/internal/auth"
	"github.com/localstack/lstk/internal/awsconfig"
	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/endpoint"
	"github.com/localstack/lstk/internal/log"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/ports"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/telemetry"
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
	Logger           log.Logger
	Telemetry        *telemetry.Client
}

func emitEmulatorStartError(ctx context.Context, tel *telemetry.Client, c runtime.ContainerConfig, errorCode, errorMsg string) {
	if tel == nil {
		return
	}
	tel.EmitEmulatorLifecycleEvent(ctx, telemetry.LifecycleEvent{
		EventType: telemetry.LifecycleStartError,
		Emulator:  c.EmulatorType,
		Image:     c.Image,
		ErrorCode: errorCode,
		ErrorMsg:  errorMsg,
	})
}

func emitEmulatorStartSuccess(ctx context.Context, tel *telemetry.Client, c runtime.ContainerConfig, containerID string, durationMS int64, pulled bool, info *telemetry.LocalStackInfo) {
	if tel == nil {
		return
	}
	tel.EmitEmulatorLifecycleEvent(ctx, telemetry.LifecycleEvent{
		EventType:      telemetry.LifecycleStartSuccess,
		Emulator:       c.EmulatorType,
		Image:          c.Image,
		ContainerID:    containerID,
		DurationMS:     durationMS,
		Pulled:         pulled,
		LocalStackInfo: info,
	})
}

func Start(ctx context.Context, rt runtime.Runtime, sink output.Sink, opts StartOptions, interactive bool) error {
	if err := rt.IsHealthy(ctx); err != nil {
		rt.EmitUnhealthyError(sink, err)
		return output.NewSilentError(fmt.Errorf("runtime not healthy: %w", err))
	}

	tokenStorage, err := auth.NewTokenStorage(opts.ForceFileKeyring, opts.Logger)
	if err != nil {
		return fmt.Errorf("failed to initialize token storage: %w", err)
	}
	a := auth.New(sink, opts.PlatformClient, tokenStorage, opts.AuthToken, opts.WebAppURL, interactive, "")

	token, err := a.GetToken(ctx)
	if err != nil {
		return err
	}

	if opts.Telemetry != nil {
		opts.Telemetry.SetAuthToken(token)
	}

	if hasDuplicateContainerTypes(opts.Containers) {
		output.EmitWarning(sink, "Multiple emulators of the same type are defined in your config; this setup is not supported yet")
	}

	tel := opts.Telemetry

	hostEnv := filterHostEnv(os.Environ())

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

		containerPort, err := c.ContainerPort()
		if err != nil {
			return err
		}

		resolvedEnv, err := c.ResolvedEnv(opts.Env)
		if err != nil {
			return err
		}

		containerName := c.Name()
		env := append(resolvedEnv,
			"LOCALSTACK_AUTH_TOKEN="+token,
			"GATEWAY_LISTEN=:4566,:443",
			"MAIN_CONTAINER_NAME="+containerName,
		)

		env = append(env, hostEnv...)

		var binds []runtime.BindMount
		if socketPath := rt.SocketPath(); socketPath != "" {
			binds = append(binds, runtime.BindMount{HostPath: socketPath, ContainerPath: "/var/run/docker.sock"})
			env = append(env, "DOCKER_HOST=unix:///var/run/docker.sock")
		}

		volumeDir, err := c.VolumeDir()
		if err != nil {
			return err
		}
		if err := os.MkdirAll(volumeDir, 0755); err != nil {
			return fmt.Errorf("failed to create volume directory %s: %w", volumeDir, err)
		}
		binds = append(binds, runtime.BindMount{HostPath: volumeDir, ContainerPath: "/var/lib/localstack"})

		containers[i] = runtime.ContainerConfig{
			Image:              image,
			Name:               containerName,
			EmulatorType:       string(c.Type),
			Port:               c.Port,
			ContainerPort:      containerPort,
			HealthPath:         healthPath,
			Env:                env,
			Tag:                c.Tag,
			ProductName: productName,
			Binds:       binds,
			ExtraPorts:         servicePortRange(),
		}
	}

	containers, err = selectContainersToStart(ctx, rt, sink, tel, containers, opts.LocalStackHost, opts.WebAppURL)
	if err != nil {
		return err
	}
	if len(containers) == 0 {
		return nil
	}

	licenseFilePath, err := config.LicenseFilePath()
	if err != nil {
		return fmt.Errorf("failed to determine license file path: %w", err)
	}

	// Validate licenses before pulling where possible (pinned tags always; "latest" tags via catalog API).
	// Returns containers that still need post-pull validation (catalog unavailable).
	postPullContainers, err := tryPrePullLicenseValidation(ctx, sink, opts, tel, containers, token, licenseFilePath)
	if err != nil {
		return err
	}

	pulled, err := pullImages(ctx, rt, sink, tel, containers)
	if err != nil {
		return err
	}

	// Catalog was unavailable for these; fall back to image inspection.
	if err := validateLicensesFromImages(ctx, rt, sink, opts, tel, postPullContainers, token, licenseFilePath); err != nil {
		return err
	}

	// Mount the cached license file into each container if available.
	if _, err := os.Stat(licenseFilePath); err == nil {
		for i := range containers {
			containers[i].Binds = append(containers[i].Binds, runtime.BindMount{
				HostPath:      licenseFilePath,
				ContainerPath: "/etc/localstack/conf.d/license.json",
				ReadOnly:      true,
			})
		}
	}

	if err := startContainers(ctx, rt, sink, tel, containers, pulled); err != nil {
		return err
	}

	// Maps emulator types to their post-start setup functions.
	// Add an entry here to run setup for a new emulator type (e.g. Azure, Snowflake).
	setups := map[config.EmulatorType]postStartSetupFunc{
		config.EmulatorAWS: awsconfig.EnsureProfile,
	}
	return runPostStartSetups(ctx, sink, opts.Containers, interactive, opts.LocalStackHost, opts.WebAppURL, setups)
}

func runPostStartSetups(ctx context.Context, sink output.Sink, containers []config.ContainerConfig, interactive bool, localStackHost, webAppURL string, setups map[config.EmulatorType]postStartSetupFunc) error {
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
				output.EmitNote(sink, endpoint.DNSRebindNote)
			}
			if err := setup(ctx, sink, interactive, resolvedHost); err != nil {
				return err
			}
			emitPostStartPointers(sink, resolvedHost, webAppURL, true)
		}
	}
	return nil
}

func emitPostStartPointers(sink output.Sink, resolvedHost, webAppURL string, showTip bool) {
	output.EmitSecondary(sink, fmt.Sprintf("• Endpoint: %s", resolvedHost))
	if webAppURL != "" {
		output.EmitSecondary(sink, fmt.Sprintf("• Web app: %s", strings.TrimRight(webAppURL, "/")))
	}
	if showTip {
		tips := []string{
			"> Tip: View emulator logs: lstk logs --follow",
			"> Tip: View deployed resources: lstk status",
		}
		output.EmitSecondary(sink, tips[rand.IntN(len(tips))])
	}
}

func pullImages(ctx context.Context, rt runtime.Runtime, sink output.Sink, tel *telemetry.Client, containers []runtime.ContainerConfig) (map[string]bool, error) {
	pulled := make(map[string]bool, len(containers))
	for _, c := range containers {
		// Remove any existing stopped container with the same name
		if err := rt.Remove(ctx, c.Name); err != nil && !errdefs.IsNotFound(err) {
			return nil, fmt.Errorf("failed to remove existing container %s: %w", c.Name, err)
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
			emitEmulatorStartError(ctx, tel, c, telemetry.ErrCodeImagePullFailed, err.Error())
			return nil, output.NewSilentError(fmt.Errorf("failed to pull image %s: %w", c.Image, err))
		}
		output.EmitSpinnerStop(sink)
		output.EmitSuccess(sink, fmt.Sprintf("Pulled %s", c.Image))
		pulled[c.Name] = true
	}
	return pulled, nil
}

// Validates licenses before pulling where the version is known.
// Pinned tags are validated immediately; "latest" tags are resolved via the catalog API.
// Returns containers that couldn't be resolved (catalog unavailable) for post-pull validation.
func tryPrePullLicenseValidation(ctx context.Context, sink output.Sink, opts StartOptions, tel *telemetry.Client, containers []runtime.ContainerConfig, token, licenseFilePath string) ([]runtime.ContainerConfig, error) {
	var needsPostPull []runtime.ContainerConfig
	for _, c := range containers {
		if c.EmulatorType == string(config.EmulatorSnowflake) {
			continue
		}

		if c.Tag != "" && c.Tag != "latest" {
			if err := validateLicense(ctx, sink, opts, tel, c, token, licenseFilePath); err != nil {
				return nil, err
			}
			continue
		}

		apiCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		v, err := opts.PlatformClient.GetLatestCatalogVersion(apiCtx, c.EmulatorType)
		cancel()

		if err != nil {
			needsPostPull = append(needsPostPull, c)
			continue
		}

		cWithVersion := c
		cWithVersion.Tag = v
		if err := validateLicense(ctx, sink, opts, tel, cWithVersion, token, licenseFilePath); err != nil {
			return nil, err
		}
	}
	return needsPostPull, nil
}

// Fallback path: inspects each pulled image for its version, then validates the license.
func validateLicensesFromImages(ctx context.Context, rt runtime.Runtime, sink output.Sink, opts StartOptions, tel *telemetry.Client, containers []runtime.ContainerConfig, token, licenseFilePath string) error {
	for _, c := range containers {
		if c.EmulatorType == string(config.EmulatorSnowflake) {
			continue
		}

		v, err := rt.GetImageVersion(ctx, c.Image)
		if err != nil {
			return fmt.Errorf("could not resolve version from image %s: %w", c.Image, err)
		}
		c.Tag = v
		if err := validateLicense(ctx, sink, opts, tel, c, token, licenseFilePath); err != nil {
			return err
		}
	}
	return nil
}

func startContainers(ctx context.Context, rt runtime.Runtime, sink output.Sink, tel *telemetry.Client, containers []runtime.ContainerConfig, pulled map[string]bool) error {
	for _, c := range containers {
		startTime := time.Now()
		output.EmitStatus(sink, "starting", c.Name, "")
		containerID, err := rt.Start(ctx, c)
		if err != nil {
			emitEmulatorStartError(ctx, tel, c, telemetry.ErrCodeStartFailed, err.Error())
			return fmt.Errorf("failed to start LocalStack: %w", err)
		}

		output.EmitStatus(sink, "waiting", c.Name, "")
		healthURL := fmt.Sprintf("http://localhost:%s%s", c.Port, c.HealthPath)
		if err := awaitStartup(ctx, rt, sink, containerID, "LocalStack", healthURL); err != nil {
			emitEmulatorStartError(ctx, tel, c, telemetry.ErrCodeStartFailed, err.Error())
			return err
		}

		output.EmitStatus(sink, "ready", c.Name, fmt.Sprintf("containerId: %s", containerID[:12]))

		lsInfo, _ := fetchLocalStackInfo(ctx, c.Port)
		emitEmulatorStartSuccess(ctx, tel, c, containerID[:12], time.Since(startTime).Milliseconds(), pulled[c.Name], lsInfo)
	}
	return nil
}

func selectContainersToStart(ctx context.Context, rt runtime.Runtime, sink output.Sink, tel *telemetry.Client, containers []runtime.ContainerConfig, localStackHost, webAppURL string) ([]runtime.ContainerConfig, error) {
	var filtered []runtime.ContainerConfig
	for _, c := range containers {
		running, err := rt.IsRunning(ctx, c.Name)
		if err != nil && !errdefs.IsNotFound(err) {
			return nil, fmt.Errorf("failed to check container status: %w", err)
		}
		if running {
			output.EmitNote(sink, "LocalStack is already running")
			resolvedHost, dnsOK := endpoint.ResolveHost(c.Port, localStackHost)
			if !dnsOK {
				output.EmitNote(sink, endpoint.DNSRebindNote)
			}
			emitPostStartPointers(sink, resolvedHost, webAppURL, c.EmulatorType == string(config.EmulatorAWS))
			continue
		}

		imageRepo, _, _ := strings.Cut(c.Image, ":")
		found, err := rt.FindRunningByImage(ctx, []string{imageRepo, "localstack/localstack"}, c.ContainerPort)
		if err != nil {
			return nil, fmt.Errorf("failed to scan for running containers: %w", err)
		}
		if found != nil {
			if found.BoundPort != c.Port {
				output.EmitError(sink, output.ErrorEvent{
					Title:   fmt.Sprintf("LocalStack is already running on port %s", found.BoundPort),
					Summary: fmt.Sprintf("Config expects port %s. Only one instance can run at a time.", c.Port),
					Actions: []output.ErrorAction{
						{Label: "Stop existing emulator:", Value: "lstk stop"},
					},
				})
				emitEmulatorStartError(ctx, tel, c, telemetry.ErrCodePortConflict, fmt.Sprintf("running on port %s, configured port %s", found.BoundPort, c.Port))
				return nil, output.NewSilentError(fmt.Errorf("LocalStack already running on port %s", found.BoundPort))
			}
			output.EmitInfo(sink, "LocalStack is already running")
			continue
		}

		if _, err := ports.CheckAvailable(c.Port); err != nil {
			if info, infoErr := fetchLocalStackInfo(ctx, c.Port); infoErr == nil {
				emitLocalStackAlreadyRunningWarning(sink, c.Port, info.Version, c.Tag)
				continue
			}
			emitPortInUseError(sink, c.Port)
			emitEmulatorStartError(ctx, tel, c, telemetry.ErrCodePortConflict, err.Error())
			return nil, output.NewSilentError(err)
		}

		// Check extra ports required by this emulator (443 for HTTPS, 4510-4559 for
		// the service port range). These are singletons: if any is taken, another
		// LocalStack instance is likely running and we cannot start a new one.
		extraSpecs := make([]string, len(c.ExtraPorts))
		for i, ep := range c.ExtraPorts {
			extraSpecs[i] = ep.HostPort
		}
		if conflictPort, err := ports.CheckAvailable(extraSpecs...); err != nil {
			output.EmitError(sink, output.ErrorEvent{
				Title:   fmt.Sprintf("Port %s is already in use", conflictPort),
				Summary: "LocalStack requires this port. Free it before starting.",
			})
			emitEmulatorStartError(ctx, tel, c, telemetry.ErrCodePortConflict, err.Error())
			return nil, output.NewSilentError(err)
		}

		filtered = append(filtered, c)
	}
	return filtered, nil
}

func emitLocalStackAlreadyRunningWarning(sink output.Sink, port, runningVersion, configTag string) {
	if configTag == "" {
		configTag = "latest"
	}
	if runningVersion != configTag {
		output.EmitWarning(sink, fmt.Sprintf(
			"LocalStack %s is already running on port %s (config specifies %s) — using the running instance",
			runningVersion, port, configTag,
		))
	} else {
		output.EmitInfo(sink, fmt.Sprintf("LocalStack %s is already running on port %s", runningVersion, port))
	}
}

func emitPortInUseError(sink output.Sink, port string) {
	actions := []output.ErrorAction{}
	configPath, pathErr := config.ConfigFilePath()
	if pathErr == nil {
		actions = append(actions, output.ErrorAction{Label: "Use another port in the configuration:", Value: configPath})
	}
	output.EmitError(sink, output.ErrorEvent{
		Title:   fmt.Sprintf("Port %s already in use", port),
		Summary: "Free the port or configure a different one.",
		Actions: actions,
	})
}

func validateLicense(ctx context.Context, sink output.Sink, opts StartOptions, tel *telemetry.Client, containerConfig runtime.ContainerConfig, token, licenseFilePath string) error {
	version := containerConfig.Tag
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

	licenseResp, err := opts.PlatformClient.GetLicense(ctx, licenseReq)
	if err != nil {
		var licErr *api.LicenseError
		if errors.As(err, &licErr) && licErr.Detail != "" {
			opts.Logger.Error("license server response (HTTP %d): %s", licErr.Status, licErr.Detail)
		}
		emitEmulatorStartError(ctx, tel, containerConfig, telemetry.ErrCodeLicenseInvalid, err.Error())
		return fmt.Errorf("license validation failed for %s:%s: %w", containerConfig.ProductName, version, err)
	}

	if licenseResp != nil && len(licenseResp.RawBytes) > 0 {
		if err := os.MkdirAll(filepath.Dir(licenseFilePath), 0755); err != nil {
			opts.Logger.Error("failed to create license cache directory: %v", err)
		} else if err := os.WriteFile(licenseFilePath, licenseResp.RawBytes, 0600); err != nil {
			opts.Logger.Error("failed to cache license file: %v", err)
		}
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

// filterHostEnv returns the subset of host environment entries that should be
// forwarded to the emulator container. It keeps CI and LOCALSTACK_* variables
// but explicitly drops LOCALSTACK_AUTH_TOKEN so the host value cannot overwrite
// the token resolved by lstk (which may come from the keyring).
func filterHostEnv(envList []string) []string {
	var out []string
	for _, e := range envList {
		if strings.HasPrefix(e, "CI=") ||
			(strings.HasPrefix(e, "LOCALSTACK_") && !strings.HasPrefix(e, "LOCALSTACK_AUTH_TOKEN=")) {
			out = append(out, e)
		}
	}
	return out
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

func servicePortRange() []runtime.PortMapping {
	const start = 4510
	const end = 4559
	ports := []runtime.PortMapping{{ContainerPort: "443", HostPort: "443"}}
	for p := start; p <= end; p++ {
		ps := strconv.Itoa(p)
		ports = append(ports, runtime.PortMapping{ContainerPort: ps, HostPort: ps})
	}
	return ports
}
