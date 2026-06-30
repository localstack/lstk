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
	"github.com/localstack/lstk/internal/caller"
	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/emulator/snowflake"
	"github.com/localstack/lstk/internal/endpoint"
	"github.com/localstack/lstk/internal/log"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/ports"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/telemetry"
	"github.com/localstack/lstk/internal/version"
)

const envPersistenceEnabled = "LOCALSTACK_PERSISTENCE=1"

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
	Persist          bool
	Logger           log.Logger
	Telemetry        *telemetry.Client
}

func Start(ctx context.Context, rt runtime.Runtime, sink output.Sink, opts StartOptions, interactive bool) (string, error) {
	if err := rt.IsHealthy(ctx); err != nil {
		rt.EmitUnhealthyError(sink, err)
		return "", output.NewSilentError(fmt.Errorf("runtime not healthy: %w", err))
	}

	tokenStorage, err := auth.NewTokenStorage(opts.ForceFileKeyring, opts.Logger)
	if err != nil {
		return "", fmt.Errorf("failed to initialize token storage: %w", err)
	}
	a := auth.New(sink, opts.PlatformClient, tokenStorage, opts.AuthToken, opts.WebAppURL, interactive, "")

	token, err := a.GetToken(ctx)
	if err != nil {
		return "", err
	}

	opts.Telemetry.SetAuthToken(token)

	if hasDuplicateContainerTypes(opts.Containers) {
		sink.Emit(output.MessageEvent{Severity: output.SeverityWarning, Text: "Multiple emulators of the same type are defined in your config; this setup is not supported yet"})
	}

	tel := opts.Telemetry

	hostEnv := filterHostEnv(os.Environ())
	agentEnvVars := agentEnv(caller.New().Classify())

	containers := make([]runtime.ContainerConfig, len(opts.Containers))
	for i, c := range opts.Containers {
		image, err := c.Image()
		if err != nil {
			return "", err
		}
		healthPath, err := c.HealthPath()
		if err != nil {
			return "", err
		}
		productName, err := c.ProductName()
		if err != nil {
			return "", err
		}

		containerPort, err := c.ContainerPort()
		if err != nil {
			return "", err
		}

		resolvedEnv, err := c.ResolvedEnv(opts.Env)
		if err != nil {
			return "", err
		}

		containerName := c.Name()
		env := append(resolvedEnv,
			"LOCALSTACK_AUTH_TOKEN="+token,
			"GATEWAY_LISTEN=:4566,:443",
			"MAIN_CONTAINER_NAME="+containerName,
			"LOCALSTACK_HOST="+endpoint.Hostname+":"+c.Port,
		)

		// The Python Snowflake emulator routes all S3 access through a single
		// SF_S3_ENDPOINT (defaulting to port 4566), so on a custom port internal
		// stages (e.g. COPY INTO) fail. Set it to match the configured port unless
		// the user already provided their own value. AWS + Snowflake multi-emulator
		// setups are not supported by this flow yet.
		if c.Type == config.EmulatorSnowflake && !envHasKey(resolvedEnv, "SF_S3_ENDPOINT") {
			env = append(env, "SF_S3_ENDPOINT="+snowflake.S3Endpoint(c.Port))
		}

		env = append(env, hostEnv...)
		env = append(env, agentEnvVars...)

		if opts.Persist {
			env = append(env, envPersistenceEnabled)
		}

		var binds []runtime.BindMount
		if socketPath := rt.SocketPath(); socketPath != "" {
			binds = append(binds, runtime.BindMount{HostPath: socketPath, ContainerPath: "/var/run/docker.sock"})
			env = append(env, "DOCKER_HOST=unix:///var/run/docker.sock")
		}

		volumeDir, err := c.VolumeDir()
		if err != nil {
			return "", err
		}
		if err := os.MkdirAll(volumeDir, 0755); err != nil {
			return "", fmt.Errorf("failed to create volume directory %s: %w", volumeDir, err)
		}
		binds = append(binds, runtime.BindMount{HostPath: volumeDir, ContainerPath: "/var/lib/localstack"})

		// Extra user-defined mounts (e.g. Snowflake init hooks). Unlike the persistence
		// directory, these are not created — init-hook entries are files, so the source
		// must already exist; creating it would produce a wrong empty directory.
		extraVolumes, err := c.ExtraVolumes()
		if err != nil {
			return "", err
		}
		for _, m := range extraVolumes {
			if _, err := os.Stat(m.Source); err != nil {
				return "", fmt.Errorf("volume source %q does not exist: %w", m.Source, err)
			}
			binds = append(binds, runtime.BindMount{HostPath: m.Source, ContainerPath: m.Target, ReadOnly: m.ReadOnly})
		}

		containers[i] = runtime.ContainerConfig{
			Image:         image,
			Name:          containerName,
			EmulatorType:  c.Type,
			Port:          c.Port,
			ContainerPort: containerPort,
			HealthPath:    healthPath,
			Env:           env,
			Tag:           c.Tag,
			ProductName:   productName,
			Binds:         binds,
			ExtraPorts:    servicePortRange(),
		}
	}

	containers, err = selectContainersToStart(ctx, rt, sink, tel, containers, opts.LocalStackHost, opts.WebAppURL)
	if err != nil {
		return "", err
	}
	if len(containers) == 0 {
		return "", nil
	}

	licenseFilePath, err := config.LicenseFilePath()
	if err != nil {
		return "", fmt.Errorf("failed to determine license file path: %w", err)
	}

	// Validate licenses before pulling. Pinned tags are validated immediately —
	// unless the image is already present locally, in which case both the pull and
	// the pre-flight check are skipped. "latest" tags defer to post-pull validation.
	postPullContainers, err := tryPrePullLicenseValidation(ctx, rt, sink, opts, containers, token, licenseFilePath)
	if err != nil {
		return "", err
	}

	pulled, err := pullImages(ctx, rt, sink, tel, containers)
	if err != nil {
		return "", err
	}

	// Validate "latest" containers by inspecting the pulled image for its version.
	resolvedVersion, err := validateLicensesFromImages(ctx, rt, sink, opts, postPullContainers, token, licenseFilePath)
	if err != nil {
		return "", err
	}

	// For pinned containers (postPullContainers was empty), use the tag directly.
	if resolvedVersion == "" {
		for _, c := range containers {
			if c.EmulatorType != config.EmulatorSnowflake && c.Tag != "" && c.Tag != "latest" {
				resolvedVersion = c.Tag
				break
			}
		}
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
		return "", err
	}

	// Maps emulator types to their post-start setup functions.
	// Add an entry here to run setup for a new emulator type (e.g. Azure, Snowflake).
	setups := map[config.EmulatorType]postStartSetupFunc{
		config.EmulatorAWS: awsconfig.EnsureProfile,
	}
	return resolvedVersion, runPostStartSetups(ctx, rt, sink, opts.Containers, interactive, opts.LocalStackHost, opts.WebAppURL, setups)
}

func runPostStartSetups(ctx context.Context, rt runtime.Runtime, sink output.Sink, containers []config.ContainerConfig, interactive bool, localStackHost, webAppURL string, setups map[config.EmulatorType]postStartSetupFunc) error {
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
		c := firstByType[t]
		resolvedHost, dnsOK := endpoint.ResolveHost(ctx, c.Port, localStackHost)
		if !dnsOK {
			sink.Emit(output.MessageEvent{Severity: output.SeverityNote, Text: endpoint.DNSRebindNote})
		}
		if setup, ok := setups[t]; ok {
			if err := setup(ctx, sink, interactive, resolvedHost); err != nil {
				return err
			}
		}
		emitPostStartPointers(sink, t, resolvedHost, webAppURL, isPersistenceEnabled(ctx, rt, c.Name()))
	}
	return nil
}

func emitAlreadyRunning(ctx context.Context, sink output.Sink, c runtime.ContainerConfig, localStackHost, webAppURL string, persist bool) {
	name := c.EmulatorType.DisplayName()
	if info, err := fetchLocalStackInfo(ctx, c.Port); err == nil && info.Version != "" {
		// /_localstack/info may report a build suffix (e.g. "2026.5.3:04ddfd3a0");
		// keep only the version number.
		version, _, _ := strings.Cut(info.Version, ":")
		name = fmt.Sprintf("%s %s", name, version)
	}
	sink.Emit(output.MessageEvent{Severity: output.SeverityNote, Text: fmt.Sprintf("%s is already running", name)})
	resolvedHost, dnsOK := endpoint.ResolveHost(ctx, c.Port, localStackHost)
	if !dnsOK {
		sink.Emit(output.MessageEvent{Severity: output.SeverityNote, Text: endpoint.DNSRebindNote})
	}
	emitPostStartPointers(sink, c.EmulatorType, resolvedHost, webAppURL, persist)
}

func isPersistenceEnabled(ctx context.Context, rt runtime.Runtime, containerName string) bool {
	env, err := rt.ContainerEnv(ctx, containerName)
	if err != nil {
		return false
	}
	return slices.Contains(env, envPersistenceEnabled)
}

func emitPostStartPointers(sink output.Sink, emulatorType config.EmulatorType, resolvedHost, webAppURL string, persist bool) {
	if sfHost := snowflake.Hostname(resolvedHost); emulatorType == config.EmulatorSnowflake && sfHost != "" {
		sink.Emit(output.MessageEvent{Severity: output.SeveritySecondary, Text: fmt.Sprintf("• Snowflake endpoint: http://%s", sfHost)})
	} else {
		sink.Emit(output.MessageEvent{Severity: output.SeveritySecondary, Text: fmt.Sprintf("• Endpoint: %s", resolvedHost)})
	}
	if persist && emulatorType == config.EmulatorAWS {
		sink.Emit(output.MessageEvent{Severity: output.SeveritySecondary, Text: "• Persistence: Enabled"})
	}
	if webAppURL != "" {
		sink.Emit(output.MessageEvent{Severity: output.SeveritySecondary, Text: fmt.Sprintf("• Web app: %s", strings.TrimRight(webAppURL, "/"))})
	}
	if tips := tipsForType(emulatorType); len(tips) > 0 {
		sink.Emit(output.MessageEvent{Severity: output.SeveritySecondary, Text: tips[rand.IntN(len(tips))]})
	}
}

func tipsForType(t config.EmulatorType) []string {
	switch t {
	case config.EmulatorAWS:
		return []string{
			"> Tip: View emulator logs: lstk logs --follow",
			"> Tip: View deployed resources: lstk status",
		}
	case config.EmulatorSnowflake:
		return []string{
			"> Tip: View emulator logs: lstk logs --follow",
			"> Tip: Check emulator status: lstk status",
		}
	case config.EmulatorAzure:
		return []string{
			"> Tip: View emulator logs: lstk logs --follow",
			"> Tip: Check emulator status: lstk status",
		}
	}
	return nil
}

func pullImages(ctx context.Context, rt runtime.Runtime, sink output.Sink, tel *telemetry.Client, containers []runtime.ContainerConfig) (map[string]bool, error) {
	pulled := make(map[string]bool, len(containers))
	for _, c := range containers {
		// Remove any existing stopped container with the same name
		if err := rt.Remove(ctx, c.Name); err != nil && !errdefs.IsNotFound(err) {
			return nil, fmt.Errorf("failed to remove existing container %s: %w", c.Name, err)
		}

		// Reuse a locally present image for pinned tags instead of re-pulling.
		// Floating "latest"/empty tags always pull until pull_policy support lands.
		if c.Tag != "" && c.Tag != "latest" {
			exists, err := rt.ImageExists(ctx, c.Image)
			if err != nil {
				return nil, fmt.Errorf("failed to check for local image %s: %w", c.Image, err)
			}
			if exists {
				sink.Emit(output.MessageEvent{Severity: output.SeveritySuccess, Text: fmt.Sprintf("Using local image %s", c.Image)})
				pulled[c.Name] = false
				continue
			}
		}

		sink.Emit(output.SpinnerStart(fmt.Sprintf("Pulling %s", c.Image)))
		sink.Emit(output.ContainerStatusEvent{Phase: "pulling", Container: c.Image})
		progress := make(chan runtime.PullProgress)
		go func() {
			for p := range progress {
				sink.Emit(output.ProgressEvent{Container: c.Image, LayerID: p.LayerID, Status: p.Status, Current: p.Current, Total: p.Total})
			}
		}()
		if err := rt.PullImage(ctx, c.Image, progress); err != nil {
			sink.Emit(output.SpinnerStop())
			// A cancelled caller context (e.g. Ctrl+C) is not an offline condition —
			// propagate it instead of probing for a local image or reporting a pull
			// failure, which would also emit a spurious start-error telemetry event.
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			// The registry may be unreachable (offline, proxy, or TLS interception in
			// enterprise networks). If the image is already available locally, fall back
			// to it instead of failing — the image carries its own license.
			if exists, existsErr := rt.ImageExists(ctx, c.Image); existsErr == nil && exists {
				sink.Emit(output.MessageEvent{Severity: output.SeverityWarning, Text: fmt.Sprintf("Could not pull %s; using the local image", c.Image)})
				continue
			}
			sink.Emit(output.ErrorEvent{
				Title:   fmt.Sprintf("Failed to pull %s", c.Image),
				Summary: err.Error(),
			})
			tel.EmitEmulatorLifecycleEvent(ctx, telemetry.LifecycleEvent{
				EventType: telemetry.LifecycleStartError,
				Emulator:  c.EmulatorType,
				Image:     c.Image,
				ErrorCode: telemetry.ErrCodeImagePullFailed,
				ErrorMsg:  err.Error(),
			})
			return nil, output.NewSilentError(fmt.Errorf("failed to pull image %s: %w", c.Image, err))
		}
		sink.Emit(output.SpinnerStop())
		sink.Emit(output.MessageEvent{Severity: output.SeveritySuccess, Text: fmt.Sprintf("Pulled %s", c.Image)})
		pulled[c.Name] = true
	}
	return pulled, nil
}

// Validates licenses before pulling for containers with pinned tags, except those
// whose image is already present locally (not pulled, so the check is skipped too).
// "latest" and empty tags are deferred to post-pull validation via image inspection.
func tryPrePullLicenseValidation(ctx context.Context, rt runtime.Runtime, sink output.Sink, opts StartOptions, containers []runtime.ContainerConfig, token, licenseFilePath string) ([]runtime.ContainerConfig, error) {
	var needsPostPull []runtime.ContainerConfig
	for _, c := range containers {
		if c.EmulatorType.SelfValidatesLicense() {
			continue
		}

		if c.Tag != "" && c.Tag != "latest" {
			// A pinned image already present locally is not pulled (see pullImages),
			// so skip the license pre-flight too: the check is redundant — and a hard
			// blocker in offline/enterprise environments — when no network round-trip
			// happens at all and the container validates its own bundled license at
			// startup. A probe error is non-fatal here; fall through to the check.
			if exists, err := rt.ImageExists(ctx, c.Image); err == nil && exists {
				continue
			}
			if err := validateLicense(ctx, sink, opts, c, token, licenseFilePath); err != nil {
				return nil, err
			}
			continue
		}

		needsPostPull = append(needsPostPull, c)
	}
	return needsPostPull, nil
}

// Inspects each pulled image for its version, then validates the license.
// Returns the resolved version of the first validated container, empty string if none.
func validateLicensesFromImages(ctx context.Context, rt runtime.Runtime, sink output.Sink, opts StartOptions, containers []runtime.ContainerConfig, token, licenseFilePath string) (string, error) {
	var firstVersion string
	for _, c := range containers {
		if c.EmulatorType.SelfValidatesLicense() {
			continue
		}

		v, err := rt.GetImageVersion(ctx, c.Image)
		if err != nil {
			return "", fmt.Errorf("could not resolve version from image %s: %w", c.Image, err)
		}
		c.Tag = v
		if firstVersion == "" {
			firstVersion = v
		}
		if err := validateLicense(ctx, sink, opts, c, token, licenseFilePath); err != nil {
			return "", err
		}
	}
	return firstVersion, nil
}

func startContainers(ctx context.Context, rt runtime.Runtime, sink output.Sink, tel *telemetry.Client, containers []runtime.ContainerConfig, pulled map[string]bool) error {
	for _, c := range containers {
		startTime := time.Now()
		sink.Emit(output.SpinnerStart("Starting LocalStack"))
		containerID, err := rt.Start(ctx, c)
		if err != nil {
			sink.Emit(output.SpinnerStop())
			tel.EmitEmulatorLifecycleEvent(ctx, telemetry.LifecycleEvent{
				EventType: telemetry.LifecycleStartError,
				Emulator:  c.EmulatorType,
				Image:     c.Image,
				ErrorCode: telemetry.ErrCodeStartFailed,
				ErrorMsg:  err.Error(),
			})
			return fmt.Errorf("failed to start LocalStack: %w", err)
		}

		healthURL := fmt.Sprintf("http://localhost:%s%s", c.Port, c.HealthPath)
		if err := awaitStartup(ctx, rt, sink, containerID, "LocalStack", healthURL); err != nil {
			sink.Emit(output.SpinnerStop())
			errCode := telemetry.ErrCodeStartFailed
			var licErr *licenseNotCoveredError
			if errors.As(err, &licErr) && c.EmulatorType.SelfValidatesLicense() {
				errCode = telemetry.ErrCodeLicenseInvalid
				sink.Emit(output.ErrorEvent{
					Title: fmt.Sprintf("Your license does not include the %s emulator.", c.EmulatorType.ShortName()),
					Actions: []output.ErrorAction{
						{Label: "Sign up for a free trial:", Value: "https://app.localstack.cloud/sign-up"},
						{Label: "Contact our team:", Value: "https://www.localstack.cloud/demo"},
					},
				})
				err = output.NewSilentError(err)
			}
			tel.EmitEmulatorLifecycleEvent(ctx, telemetry.LifecycleEvent{
				EventType: telemetry.LifecycleStartError,
				Emulator:  c.EmulatorType,
				Image:     c.Image,
				ErrorCode: errCode,
				ErrorMsg:  err.Error(),
			})
			return err
		}
		sink.Emit(output.SpinnerStop())

		sink.Emit(output.ContainerStatusEvent{Phase: "ready", Container: c.Name, Detail: fmt.Sprintf("containerId: %s", containerID[:12])})

		lsInfo, _ := fetchLocalStackInfo(ctx, c.Port)
		tel.EmitEmulatorLifecycleEvent(ctx, telemetry.LifecycleEvent{
			EventType:      telemetry.LifecycleStartSuccess,
			Emulator:       c.EmulatorType,
			Image:          c.Image,
			ContainerID:    containerID[:12],
			DurationMS:     time.Since(startTime).Milliseconds(),
			Pulled:         pulled[c.Name],
			LocalStackInfo: lsInfo,
		})
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
			emitAlreadyRunning(ctx, sink, c, localStackHost, webAppURL, isPersistenceEnabled(ctx, rt, c.Name))
			continue
		}

		found, err := rt.FindRunningByImage(ctx, config.KnownImageRepos(), c.ContainerPort)
		if err != nil {
			return nil, fmt.Errorf("failed to scan for running containers: %w", err)
		}
		if found != nil {
			foundType := config.EmulatorTypeForImage(found.Image)
			if foundType != "" && foundType != c.EmulatorType {
				sink.Emit(output.ErrorEvent{
					Title:   fmt.Sprintf("%s is running on port %s", foundType.DisplayName(), found.BoundPort),
					Summary: fmt.Sprintf("Your config specifies the %s. Only one emulator can run on a port at a time.", c.EmulatorType.DisplayName()),
					Actions: []output.ErrorAction{
						{Label: "Stop the running emulator:", Value: fmt.Sprintf("docker stop %s", found.Name)},
					},
				})
				tel.EmitEmulatorLifecycleEvent(ctx, telemetry.LifecycleEvent{
					EventType: telemetry.LifecycleStartError,
					Emulator:  c.EmulatorType,
					Image:     c.Image,
					ErrorCode: telemetry.ErrCodeEmulatorMismatch,
					ErrorMsg:  fmt.Sprintf("running %s on port %s, configured %s", foundType, found.BoundPort, c.EmulatorType),
				})
				return nil, output.NewSilentError(fmt.Errorf("%s is already running on port %s", foundType.DisplayName(), found.BoundPort))
			}
			if found.BoundPort != c.Port {
				sink.Emit(output.ErrorEvent{
					Title:   fmt.Sprintf("%s is already running on port %s", c.EmulatorType.DisplayName(), found.BoundPort),
					Summary: fmt.Sprintf("Config expects port %s. Only one instance can run at a time.", c.Port),
					Actions: []output.ErrorAction{
						{Label: "Stop existing emulator:", Value: "lstk stop"},
					},
				})
				tel.EmitEmulatorLifecycleEvent(ctx, telemetry.LifecycleEvent{
					EventType: telemetry.LifecycleStartError,
					Emulator:  c.EmulatorType,
					Image:     c.Image,
					ErrorCode: telemetry.ErrCodePortConflict,
					ErrorMsg:  fmt.Sprintf("running on port %s, configured port %s", found.BoundPort, c.Port),
				})
				return nil, output.NewSilentError(fmt.Errorf("LocalStack already running on port %s", found.BoundPort))
			}
			emitAlreadyRunning(ctx, sink, c, localStackHost, webAppURL, isPersistenceEnabled(ctx, rt, found.Name))
			continue
		}

		if _, err := ports.CheckAvailable(c.Port); err != nil {
			if info, infoErr := fetchLocalStackInfo(ctx, c.Port); infoErr == nil {
				emitLocalStackAlreadyRunningWarning(sink, c.Port, info.Version, c.Tag)
				continue
			}
			emitPortInUseError(sink, c.Port)
			tel.EmitEmulatorLifecycleEvent(ctx, telemetry.LifecycleEvent{
				EventType: telemetry.LifecycleStartError,
				Emulator:  c.EmulatorType,
				Image:     c.Image,
				ErrorCode: telemetry.ErrCodePortConflict,
				ErrorMsg:  err.Error(),
			})
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
			sink.Emit(output.ErrorEvent{
				Title:   fmt.Sprintf("Port %s is already in use", conflictPort),
				Summary: "LocalStack requires this port. Free it before starting.",
			})
			tel.EmitEmulatorLifecycleEvent(ctx, telemetry.LifecycleEvent{
				EventType: telemetry.LifecycleStartError,
				Emulator:  c.EmulatorType,
				Image:     c.Image,
				ErrorCode: telemetry.ErrCodePortConflict,
				ErrorMsg:  err.Error(),
			})
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
		sink.Emit(output.MessageEvent{Severity: output.SeverityWarning, Text: fmt.Sprintf(
			"LocalStack %s is already running on port %s (config specifies %s) — using the running instance",
			runningVersion, port, configTag,
		)})
	} else {
		sink.Emit(output.MessageEvent{Severity: output.SeverityInfo, Text: fmt.Sprintf("LocalStack %s is already running on port %s", runningVersion, port)})
	}
}

func emitPortInUseError(sink output.Sink, port string) {
	actions := []output.ErrorAction{}
	configPath, pathErr := config.ConfigFilePath()
	if pathErr == nil {
		actions = append(actions, output.ErrorAction{Label: "Use another port in the configuration:", Value: configPath})
	}
	sink.Emit(output.ErrorEvent{
		Title:   fmt.Sprintf("Port %s already in use", port),
		Summary: "Free the port or configure a different one.",
		Actions: actions,
	})
}

func validateLicense(ctx context.Context, sink output.Sink, opts StartOptions, containerConfig runtime.ContainerConfig, token, licenseFilePath string) error {
	version := containerConfig.Tag
	sink.Emit(output.SpinnerStart("Checking license"))

	hostname, _ := os.Hostname()
	licenseReq := &api.LicenseRequest{
		Product: api.ProductInfo{
			Name:    containerConfig.ProductName,
			Version: config.NormalizeTag(version),
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
		sink.Emit(output.SpinnerStop())
		// A cancelled caller context (e.g. Ctrl+C) is not an offline condition —
		// propagate it instead of degrading. The client's own request timeout is
		// distinct from ctx and still falls through to the offline fallback below.
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var licErr *api.LicenseError
		if !errors.As(err, &licErr) {
			// The license server responded with no definitive verdict — the request
			// itself failed (offline, proxy, or TLS interception in enterprise
			// networks). Skip the pre-flight check and let the container validate its
			// own bundled license at startup instead of blocking the start.
			opts.Logger.Info("license server unreachable, continuing with the image's bundled license: %v", err)
			sink.Emit(output.MessageEvent{Severity: output.SeverityWarning, Text: "Could not reach the license server; continuing with the image's bundled license"})
			return nil
		}
		// Known limitation: any *api.LicenseError — i.e. any non-200 HTTP response,
		// including a 5xx or a 407 from a corporate proxy — is treated as a definitive
		// verdict and stays fatal here; only connection-level failures (handled above)
		// degrade. Gating this on licErr.Status is tracked as follow-up.
		if licErr.Detail != "" {
			opts.Logger.Error("license server response (HTTP %d): %s", licErr.Status, licErr.Detail)
		}
		if licErr.IsUnsupportedTag {
			err = errors.New(config.UnsupportedTagMessage())
		}
		opts.Telemetry.EmitEmulatorLifecycleEvent(ctx, telemetry.LifecycleEvent{
			EventType: telemetry.LifecycleStartError,
			Emulator:  containerConfig.EmulatorType,
			Image:     containerConfig.Image,
			ErrorCode: telemetry.ErrCodeLicenseInvalid,
			ErrorMsg:  err.Error(),
		})
		return fmt.Errorf("license validation failed for %s:%s: %w", containerConfig.ProductName, version, err)
	}
	sink.Emit(output.SpinnerStop())

	validMsg := "Valid license"
	if plan := licenseResp.PlanDisplayName(); plan != "" {
		validMsg = fmt.Sprintf("Valid license (%s)", plan)
	}
	sink.Emit(output.MessageEvent{Severity: output.SeveritySuccess, Text: validMsg})

	if licenseResp != nil && len(licenseResp.RawBytes) > 0 {
		if err := os.MkdirAll(filepath.Dir(licenseFilePath), 0755); err != nil {
			opts.Logger.Error("failed to create license cache directory: %v", err)
		} else if err := os.WriteFile(licenseFilePath, licenseResp.RawBytes, 0600); err != nil {
			opts.Logger.Error("failed to cache license file: %v", err)
		}
	}

	return nil
}

// licenseNotCoveredError is returned by awaitStartup when the container exits
// because the license does not include the emulator (Snowflake or Azure).
type licenseNotCoveredError struct{}

func (e *licenseNotCoveredError) Error() string {
	return "license does not include this emulator"
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
			if logsErr == nil && strings.Contains(logs, "not covered by your license") {
				return &licenseNotCoveredError{}
			}
			if logsErr != nil || logs == "" {
				return fmt.Errorf("%s exited unexpectedly", name)
			}
			return fmt.Errorf("%s exited unexpectedly:\n%s", name, logs)
		}

		resp, err := client.Get(healthURL)
		if err == nil && resp.StatusCode == http.StatusOK {
			if err := resp.Body.Close(); err != nil {
				sink.Emit(output.MessageEvent{Severity: output.SeverityWarning, Text: fmt.Sprintf("failed to close response body: %v", err)})
			}
			return nil
		}
		if resp != nil {
			if err := resp.Body.Close(); err != nil {
				sink.Emit(output.MessageEvent{Severity: output.SeverityWarning, Text: fmt.Sprintf("failed to close response body: %v", err)})
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

func envHasKey(env []string, key string) bool {
	prefix := key + "="
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			return true
		}
	}
	return false
}

func agentEnv(cl caller.Classification) []string {
	var env []string
	if cl.AgentIdentity != "" {
		env = append(env, "AI_AGENT="+cl.AgentIdentity)
	}
	env = append(env, "LOCALSTACK_CLIENT_NAME=lstk", "LOCALSTACK_CLIENT_VERSION="+version.Version())
	return env
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
