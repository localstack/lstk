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
	StartupTimeout   time.Duration // zero uses the per-mode default (resolveStartupTimeout)
	Logger           log.Logger
	Telemetry        *telemetry.Client
	// AuthOptions is passed through to auth.New; tests use it to inject a fake
	// browser opener so a re-login flow never opens a real tab.
	AuthOptions []auth.Option
}

func Start(ctx context.Context, rt runtime.Runtime, sink output.Sink, opts StartOptions, interactive bool) (string, error) {
	// Fail fast on unsupported multi-container configs before any health/auth
	// checks or image pulls, so we don't leave a partial startup that later dies
	// on container-name conflicts or shared port collisions.
	if err := checkSingleContainer(opts.Containers); err != nil {
		sink.Emit(output.ErrorEvent{
			Title:   "Unsupported configuration",
			Summary: err.Error(),
			Actions: []output.ErrorAction{{Label: "Edit your config file so only one [[containers]] block is enabled:", Value: "lstk config path"}},
		})
		return "", output.NewSilentError(err)
	}

	if err := rt.IsHealthy(ctx); err != nil {
		rt.EmitUnhealthyError(sink, err)
		return "", output.NewSilentError(fmt.Errorf("runtime not healthy: %w", err))
	}

	licenseFilePath, err := config.LicenseFilePath()
	if err != nil {
		return "", fmt.Errorf("failed to determine license file path: %w", err)
	}

	tokenStorage, err := auth.NewTokenStorage(opts.ForceFileKeyring, opts.Logger)
	if err != nil {
		return "", fmt.Errorf("failed to initialize token storage: %w", err)
	}
	a := auth.New(sink, opts.PlatformClient, tokenStorage, opts.AuthToken, opts.WebAppURL, interactive, licenseFilePath, opts.AuthOptions...)

	token, err := a.GetToken(ctx)
	if err != nil {
		return "", err
	}

	opts.Telemetry.SetAuthToken(token)

	version, err := startOnce(ctx, rt, sink, opts, interactive, token, licenseFilePath, false)
	var rejErr *licenseRejectedError
	if err == nil || !errors.As(err, &rejErr) {
		return version, err
	}

	// The platform definitively rejected the token/license (validateLicense has
	// already dropped the cached license.json). The rejected token may simply
	// predate a license purchase or plan change (DEVX-658), so offer a fresh
	// login instead of requiring a manual `lstk logout` before the next run.
	if interactive && promptRelogin(ctx, sink, rejErr.licErr) {
		newToken, loginErr := a.Relogin(ctx)
		if loginErr != nil {
			return "", loginErr
		}
		opts.Telemetry.SetAuthToken(newToken)
		version, err = startOnce(ctx, rt, sink, opts, interactive, newToken, licenseFilePath, true)
		if err == nil || !errors.As(err, &rejErr) {
			return version, err
		}
		// The freshly logged-in token was rejected too: render it the same way
		// as a first rejection instead of surfacing the raw error, below.
	}

	return "", renderLicenseRejection(sink, rejErr, err)
}

// renderLicenseRejection emits the actionable ErrorEvent for a definitive
// license rejection and returns a silent error wrapping err, so a rejection
// renders identically whether it's the initial failure or a retry after
// re-login came back rejected too.
func renderLicenseRejection(sink output.Sink, rejErr *licenseRejectedError, err error) error {
	sink.Emit(output.ErrorEvent{
		Title: fmt.Sprintf("License validation failed for %s:%s: %s", rejErr.productName, rejErr.version, rejErr.licErr.Message),
		Actions: []output.ErrorAction{
			{Label: "Log in again to refresh your credentials:", Value: "lstk logout && lstk login"},
			{Label: "Or provide a valid token via the environment variable:", Value: "LOCALSTACK_AUTH_TOKEN"},
		},
	})
	return output.NewSilentError(err)
}

// licenseRejectedError carries the product/version context of a definitive
// license rejection so the top-level handler can render it (and offer the
// re-login recovery) without re-deriving the container details.
type licenseRejectedError struct {
	productName string
	version     string
	licErr      *api.LicenseError
}

func (e *licenseRejectedError) Error() string {
	return fmt.Sprintf("license validation failed for %s:%s: %v", e.productName, e.version, e.licErr)
}

func (e *licenseRejectedError) Unwrap() error { return e.licErr }

func startOnce(ctx context.Context, rt runtime.Runtime, sink output.Sink, opts StartOptions, interactive bool, token, licenseFilePath string, forceLicenseValidation bool) (string, error) {
	tel := opts.Telemetry

	hostEnv, droppedEnv := filterHostEnv(os.Environ())
	for _, d := range droppedEnv {
		text := fmt.Sprintf("Not forwarding %s to the emulator: it would override %s inside the emulator", d.name, d.overrides)
		if d.overrides == "" {
			text = fmt.Sprintf("Not forwarding %s to the emulator: its value contains line breaks", d.name)
		}
		sink.Emit(output.MessageEvent{Severity: output.SeverityWarning, Text: text})
	}
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

		// GATEWAY_LISTEN is configurable via the [env.*] profiles. It controls
		// which ports the gateway listens on inside the container and, through
		// the host part of its first entry, which host IP published ports bind
		// to (e.g. "0.0.0.0:4566,0.0.0.0:443" exposes the emulator beyond
		// loopback). When unset it defaults to ":4566,:443" bound to loopback.
		gateway, err := parseGatewayListen(envValue(resolvedEnv, "GATEWAY_LISTEN"))
		if err != nil {
			return "", err
		}

		containerName := c.Name()
		env := append(resolvedEnv,
			"LOCALSTACK_AUTH_TOKEN="+token,
			"GATEWAY_LISTEN="+gateway.containerEnvValue(),
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

		// The primary edge port is published via the configured host port (c.Port);
		// any further gateway ports (443, and e.g. 8443) plus the service port range
		// are published host-port == container-port.
		primaryPort, _, _ := strings.Cut(containerPort, "/")
		extraPorts := append(gateway.extraGatewayPorts(primaryPort), servicePortRange()...)

		containers[i] = runtime.ContainerConfig{
			Image:         image,
			Name:          containerName,
			EmulatorType:  c.Type,
			Port:          c.Port,
			ContainerPort: containerPort,
			BindHost:      gateway.bindHost(),
			HealthPath:    healthPath,
			Env:           env,
			Tag:           c.Tag,
			ProductName:   productName,
			Binds:         binds,
			ExtraPorts:    extraPorts,
		}
	}

	containers, err := selectContainersToStart(ctx, rt, sink, tel, containers, opts.LocalStackHost, opts.WebAppURL)
	if err != nil {
		return "", err
	}
	if len(containers) == 0 {
		return "", nil
	}

	// Validate licenses before pulling. Pinned tags are validated immediately —
	// unless the image is already present locally, in which case both the pull and
	// the pre-flight check are skipped. "latest" tags defer to post-pull validation.
	postPullContainers, prePullRefreshed, err := tryPrePullLicenseValidation(ctx, rt, sink, opts, containers, token, licenseFilePath, forceLicenseValidation)
	if err != nil {
		return "", err
	}

	pulled, err := pullImages(ctx, rt, sink, tel, containers, interactive)
	if err != nil {
		return "", err
	}

	// Validate "latest" containers by inspecting the pulled image for its version.
	resolvedVersion, postPullRefreshed, err := validateLicensesFromImages(ctx, rt, sink, opts, postPullContainers, token, licenseFilePath)
	if err != nil {
		return "", err
	}
	licenseRefreshed := prePullRefreshed || postPullRefreshed

	// For pinned containers (postPullContainers was empty), use the tag directly.
	if resolvedVersion == "" {
		for _, c := range containers {
			if c.EmulatorType != config.EmulatorSnowflake && c.Tag != "" && c.Tag != "latest" {
				resolvedVersion = c.Tag
				break
			}
		}
	}

	if err := startWithLicenseRetry(ctx, rt, sink, opts, interactive, containers, pulled, token, licenseFilePath, licenseRefreshed); err != nil {
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

func pullImages(ctx context.Context, rt runtime.Runtime, sink output.Sink, tel *telemetry.Client, containers []runtime.ContainerConfig, interactive bool) (map[string]bool, error) {
	pulled := make(map[string]bool, len(containers))
	for _, c := range containers {
		// Remove any existing container with the same name. rt.Remove tolerates the
		// container being absent or mid auto-removal (--rm) and waits until it is gone.
		if err := rt.Remove(ctx, c.Name); err != nil {
			return nil, fmt.Errorf("failed to remove existing container %s: %w", c.Name, err)
		}

		exists, err := rt.ImageExists(ctx, c.Image)
		if err != nil {
			return nil, fmt.Errorf("failed to check for local image %s: %w", c.Image, err)
		}

		// Reuse a locally present image for pinned tags instead of re-pulling.
		// Floating "latest"/empty tags always pull until pull_policy support lands.
		if exists && c.Tag != "" && c.Tag != "latest" {
			sink.Emit(output.MessageEvent{Severity: output.SeveritySuccess, Text: fmt.Sprintf("Using local image %s", c.Image)})
			pulled[c.Name] = false
			continue
		}

		usedLocal, err := pullImage(ctx, rt, sink, tel, c, exists, interactive)
		if err != nil {
			return nil, err
		}
		pulled[c.Name] = !usedLocal
	}
	return pulled, nil
}

// pullImage pulls c.Image, with a graceful fall-back to an already-present local
// image. When a local copy exists and we're interactive, the user can press ESC
// to abandon the in-flight pull and keep the current image; the same fall-back
// happens automatically when the pull fails (e.g. offline). Returns usedLocal=true
// when the pull was skipped or failed and the local image is used instead.
func pullImage(ctx context.Context, rt runtime.Runtime, sink output.Sink, tel *telemetry.Client, c runtime.ContainerConfig, localExists, interactive bool) (usedLocal bool, err error) {
	sink.Emit(output.SpinnerStart(fmt.Sprintf("Pulling %s", c.Image)))
	sink.Emit(output.ContainerStatusEvent{Phase: "pulling", Container: c.Image})

	pullCtx, cancelPull := context.WithCancel(ctx)
	defer cancelPull()

	// skipCh is signaled by the TUI when the user presses ESC during the pull.
	// Buffered so the TUI never blocks if the pull has already finished.
	skipCh := make(chan struct{}, 1)
	skippable := localExists && interactive

	progress := make(chan runtime.PullProgress)
	progressDone := make(chan struct{})
	go func() {
		defer close(progressDone)
		armed := false
		for p := range progress {
			// Surface the ESC hint only once real layer download begins, so it
			// never flashes on an "up to date" / cache-hit pull.
			if skippable && !armed && p.Status == "Downloading" {
				armed = true
				sink.Emit(output.PullSkippableEvent{Image: c.Image, SkipCh: skipCh})
			}
			sink.Emit(output.ProgressEvent{Container: c.Image, LayerID: p.LayerID, Status: p.Status, Current: p.Current, Total: p.Total})
		}
	}()

	pullErrCh := make(chan error, 1)
	go func() { pullErrCh <- rt.PullImage(pullCtx, c.Image, progress) }()

	select {
	case err = <-pullErrCh:
		<-progressDone
	case <-skipCh:
		cancelPull()
		<-pullErrCh // let the pull goroutine unwind after cancellation
		<-progressDone
		sink.Emit(output.SpinnerStop())
		sink.Emit(output.MessageEvent{Severity: output.SeverityNote, Text: fmt.Sprintf("Keeping current local image %s", c.Image)})
		return true, nil
	}

	sink.Emit(output.SpinnerStop())

	if err != nil {
		// A cancelled parent context (e.g. Ctrl+C) is a deliberate abort, not a pull
		// failure: propagate it so the start flow stops instead of silently falling
		// back to the local image. (ESC-to-skip is handled by the skipCh branch above.)
		if errors.Is(err, context.Canceled) {
			return false, err
		}
		// Auto fall-back: a failed pull with a local copy present (e.g. offline) is
		// not fatal — start with the image we have and tell the user.
		if localExists {
			sink.Emit(output.MessageEvent{Severity: output.SeverityNote, Text: fmt.Sprintf("Could not pull %s (%v); using the local image", c.Image, err)})
			return true, nil
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
		return false, output.NewSilentError(fmt.Errorf("failed to pull image %s: %w", c.Image, err))
	}

	sink.Emit(output.MessageEvent{Severity: output.SeveritySuccess, Text: fmt.Sprintf("Pulled %s", c.Image)})
	return false, nil
}

// Validates licenses before pulling for containers with pinned tags, except those
// whose image is already present locally (not pulled, so the check is skipped too —
// unless force is set, e.g. when retrying after a startup license failure).
// "latest" and empty tags are deferred to post-pull validation via image inspection.
// The bool reports whether any validation refreshed the cached license file.
func tryPrePullLicenseValidation(ctx context.Context, rt runtime.Runtime, sink output.Sink, opts StartOptions, containers []runtime.ContainerConfig, token, licenseFilePath string, force bool) ([]runtime.ContainerConfig, bool, error) {
	var needsPostPull []runtime.ContainerConfig
	var refreshed bool
	for _, c := range containers {
		if c.EmulatorType.SelfValidatesLicense() {
			continue
		}

		if c.Tag != "" && c.Tag != "latest" {
			// A pinned image already present locally is not pulled (see pullImages),
			// so skip the license pre-flight too: the check is redundant — and a hard
			// blocker in offline/enterprise environments — when no network round-trip
			// happens at all and the container validates the license at
			// startup. A probe error is non-fatal here; fall through to the check.
			if !force {
				if exists, err := rt.ImageExists(ctx, c.Image); err == nil && exists {
					continue
				}
			}
			wrote, err := validateLicense(ctx, sink, opts, c, token, licenseFilePath)
			if err != nil {
				return nil, false, err
			}
			refreshed = refreshed || wrote
			continue
		}

		needsPostPull = append(needsPostPull, c)
	}
	return needsPostPull, refreshed, nil
}

// Inspects each pulled image for its version, then validates the license.
// Returns the resolved version of the first validated container (empty string if
// none) and whether any validation refreshed the cached license file.
func validateLicensesFromImages(ctx context.Context, rt runtime.Runtime, sink output.Sink, opts StartOptions, containers []runtime.ContainerConfig, token, licenseFilePath string) (string, bool, error) {
	var firstVersion string
	var refreshed bool
	for _, c := range containers {
		if c.EmulatorType.SelfValidatesLicense() {
			continue
		}

		v, err := rt.GetImageVersion(ctx, c.Image)
		if err != nil {
			return "", false, fmt.Errorf("could not resolve version from image %s: %w", c.Image, err)
		}
		c.Tag = v
		if firstVersion == "" {
			firstVersion = v
		}
		wrote, err := validateLicense(ctx, sink, opts, c, token, licenseFilePath)
		if err != nil {
			return "", false, err
		}
		refreshed = refreshed || wrote
	}
	return firstVersion, refreshed, nil
}

// startWithLicenseRetry mounts the cached license file and starts the
// containers. When a container exits with a license failure while a cached
// license.json that this run did not refresh was mounted (the pre-flight was
// skipped, e.g. because the image was already local), the cache may predate a
// license purchase or plan change (DEVX-658): it is dropped, re-validated
// against the license server, and the start is retried once.
func startWithLicenseRetry(ctx context.Context, rt runtime.Runtime, sink output.Sink, opts StartOptions, interactive bool, containers []runtime.ContainerConfig, pulled map[string]bool, token, licenseFilePath string, licenseRefreshed bool) error {
	// A retry only makes sense when a cached license this run did not refresh
	// was mounted — a freshly fetched license failing at startup is a real
	// verdict, and refetching it would loop for nothing.
	licenseMounted := mountCachedLicense(containers, licenseFilePath)
	retryCandidate := licenseMounted && !licenseRefreshed

	err := startContainers(ctx, rt, sink, opts.Telemetry, containers, pulled, opts.StartupTimeout, interactive, retryCandidate)
	if err == nil {
		return nil
	}
	var licStartErr *licenseStartupError
	if !errors.As(err, &licStartErr) {
		return err
	}

	sink.Emit(output.MessageEvent{Severity: output.SeverityWarning, Text: "License rejected at startup — refreshing the cached license and retrying"})
	if rmErr := os.Remove(licenseFilePath); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
		opts.Logger.Error("failed to remove cached license file: %v", rmErr)
	}
	retryPostPull, _, verr := tryPrePullLicenseValidation(ctx, rt, sink, opts, containers, token, licenseFilePath, true)
	if verr != nil {
		return verr
	}
	if _, _, verr := validateLicensesFromImages(ctx, rt, sink, opts, retryPostPull, token, licenseFilePath); verr != nil {
		return verr
	}
	stripLicenseMount(containers)
	mountCachedLicense(containers, licenseFilePath)
	return startContainers(ctx, rt, sink, opts.Telemetry, containers, pulled, opts.StartupTimeout, interactive, false)
}

func startContainers(ctx context.Context, rt runtime.Runtime, sink output.Sink, tel *telemetry.Client, containers []runtime.ContainerConfig, pulled map[string]bool, startupTimeout time.Duration, interactive bool, licenseRetryCandidate bool) error {
	monitor := newStartupMonitor(rt, sink, tel, startupTimeout, interactive)
	for _, c := range containers {
		startTime := time.Now()
		sink.Emit(output.SpinnerStart("Starting LocalStack"))
		containerID, exitCh, err := rt.Start(ctx, c)
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

		// Follow the container's logs into a bounded buffer from the moment it
		// starts. With AutoRemove (--rm) the container is removed the instant it
		// exits, so a post-hoc log fetch would race the removal; buffering as it
		// runs keeps the startup logs available to explain a crash.
		startupLogs := newLogTail(maxStartupLogBytes)
		logCtx, stopLogTail := context.WithCancel(ctx)
		logDone := make(chan struct{})
		go func() {
			defer close(logDone)
			_ = rt.StreamLogs(logCtx, containerID, startupLogs, true, "all")
		}()

		healthURL := fmt.Sprintf("http://localhost:%s%s", c.Port, c.HealthPath)
		err = monitor.await(ctx, containerID, healthURL, exitCh)
		// Stop following and let the goroutine return before continuing, so it does
		// not outlive the start. Bounded so a slow stream teardown can't hang start.
		stopLogTail()
		select {
		case <-logDone:
		case <-time.After(2 * time.Second):
		}
		if err != nil {
			sink.Emit(output.SpinnerStop())
			// A cancelled context (e.g. Ctrl+C) is a deliberate abort, not a
			// startup failure: propagate it without a styled error. The container
			// is detached and stays running. It is still tracked as a start_error
			// lifecycle event (matching the behavior before startup monitoring
			// was introduced) so interrupted starts stay visible in telemetry.
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				tel.EmitEmulatorLifecycleEvent(ctx, telemetry.LifecycleEvent{
					EventType: telemetry.LifecycleStartError,
					Emulator:  c.EmulatorType,
					Image:     c.Image,
					ErrorCode: telemetry.ErrCodeStartFailed,
					ErrorMsg:  err.Error(),
				})
				return err
			}
			// Read the logs only now, after the follow-goroutine's final flush, so
			// the tail is complete. Fall back to a direct fetch if nothing was
			// streamed (unlikely once the container ran).
			logs := startupLogs.String()
			if logs == "" {
				if direct, derr := rt.Logs(ctx, containerID, 20); derr == nil {
					logs = direct
				}
			}
			// When the caller can retry with a refreshed license (a stale cached
			// license.json was mounted, DEVX-658), return the classification
			// instead of rendering the failure — startWithLicenseRetry retries
			// once, and a repeat failure comes back through handleFailure. The
			// self-validating "not covered" case keeps its dedicated messaging.
			var exitErr *containerExitedError
			notCovered := c.EmulatorType.SelfValidatesLicense() && strings.Contains(logs, "not covered by your license")
			if licenseRetryCandidate && !notCovered && errors.As(err, &exitErr) && logsIndicateLicenseFailure(logs) {
				tel.EmitEmulatorLifecycleEvent(ctx, telemetry.LifecycleEvent{
					EventType: telemetry.LifecycleStartError,
					Emulator:  c.EmulatorType,
					Image:     c.Image,
					ErrorCode: telemetry.ErrCodeLicenseInvalid,
					ErrorMsg:  err.Error(),
				})
				return &licenseStartupError{name: "LocalStack", logs: logs}
			}
			return monitor.handleFailure(ctx, c, err, logs)
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

// handleFailure classifies an await failure for container c, emits the
// matching ErrorEvent + lifecycle telemetry, and returns a silent error (so the
// top-level handler does not re-print it). logs is the container's buffered
// startup output, read after the follow-goroutine's final flush.
func (m *startupMonitor) handleFailure(ctx context.Context, c runtime.ContainerConfig, err error, logs string) error {
	errCode := telemetry.ErrCodeStartFailed

	switch {
	// A self-validating emulator (Snowflake/Azure) whose license does not cover
	// it exits with a distinctive log line. Preserve the dedicated messaging.
	case c.EmulatorType.SelfValidatesLicense() && strings.Contains(logs, "not covered by your license"):
		errCode = telemetry.ErrCodeLicenseInvalid
		m.sink.Emit(output.ErrorEvent{
			Title: fmt.Sprintf("Your license does not include the %s emulator.", c.EmulatorType.ShortName()),
			Actions: []output.ErrorAction{
				{Label: "Sign up for a free trial:", Value: "https://app.localstack.cloud/sign-up"},
				{Label: "Contact our team:", Value: "https://www.localstack.cloud/demo"},
			},
		})
		err = &licenseNotCoveredError{}

	case isStartupTimeout(err):
		errCode = telemetry.ErrCodeStartTimeout
		var timeoutErr *startupTimeoutError
		errors.As(err, &timeoutErr)
		summary := "LocalStack is still running so you can inspect it, or stop it."
		actions := []output.ErrorAction{
			{Label: "View the logs:", Value: "lstk logs"},
			{Label: "Stop LocalStack:", Value: "lstk stop"},
			{Label: "Allow more time on a slow machine:", Value: "LSTK_STARTUP_TIMEOUT=5m lstk start"},
		}
		if timeoutErr != nil && timeoutErr.stopped {
			summary = "LocalStack has been stopped."
			actions = []output.ErrorAction{
				{Label: "Try again:", Value: "lstk start"},
				{Label: "Allow more time on a slow machine:", Value: "LSTK_STARTUP_TIMEOUT=5m lstk start"},
			}
		}
		if tail := lastLogLines(logs, 15); tail != "" {
			summary += "\nLast container output:\n" + tail
		}
		m.sink.Emit(output.ErrorEvent{
			Title:   err.Error(),
			Summary: summary,
			Actions: actions,
		})

	default:
		// Container exited before becoming healthy (crash during startup).
		summary := ""
		if tail := lastLogLines(logs, 15); tail != "" {
			summary = "Last container output:\n" + tail
		}
		m.sink.Emit(output.ErrorEvent{
			Title:   err.Error(),
			Summary: summary,
			Actions: []output.ErrorAction{
				{Label: "Check your configuration and try again:", Value: "lstk start"},
			},
		})
	}

	m.tel.EmitEmulatorLifecycleEvent(ctx, telemetry.LifecycleEvent{
		EventType: telemetry.LifecycleStartError,
		Emulator:  c.EmulatorType,
		Image:     c.Image,
		ErrorCode: errCode,
		ErrorMsg:  err.Error(),
	})
	return output.NewSilentError(err)
}

func isStartupTimeout(err error) bool {
	var timeoutErr *startupTimeoutError
	return errors.As(err, &timeoutErr)
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
		// the service port range). A running LocalStack was already ruled out above
		// by FindRunningByImage, so a conflict here means an unrelated process holds
		// the port — we can't publish it, so we stop rather than fail at Docker bind.
		extraSpecs := make([]string, len(c.ExtraPorts))
		for i, ep := range c.ExtraPorts {
			extraSpecs[i] = ep.HostPort
		}
		if conflictPort, err := ports.CheckAvailable(extraSpecs...); err != nil {
			emitPortInUseError(sink, conflictPort)
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
	actions := []output.ErrorAction{
		{Label: "Identify the process using it:", Value: ports.InspectCommand(port)},
	}
	configPath, pathErr := config.ConfigFilePath()
	if pathErr == nil {
		actions = append(actions, output.ErrorAction{Label: "Or use another port in the configuration:", Value: configPath})
	}
	sink.Emit(output.ErrorEvent{
		Title:   fmt.Sprintf("Port %s already in use", port),
		Summary: "Another process is already using this port.",
		Actions: actions,
	})
}

// validateLicense runs the license pre-flight and caches the license file on
// success. The bool reports whether the cached license file was (re)written.
func validateLicense(ctx context.Context, sink output.Sink, opts StartOptions, containerConfig runtime.ContainerConfig, token, licenseFilePath string) (bool, error) {
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
			return false, ctx.Err()
		}
		var licErr *api.LicenseError
		if !errors.As(err, &licErr) {
			// The license server responded with no definitive verdict — the request
			// itself failed (offline, proxy, or TLS interception in enterprise
			// networks). Skip the pre-flight check and let the container validate
			// the license at startup instead of blocking the start.
			opts.Logger.Info("license server unreachable, deferring license validation to the emulator: %v", err)
			sink.Emit(output.MessageEvent{Severity: output.SeverityWarning, Text: "Could not reach the license server; the emulator will validate the license once it starts"})
			return false, nil
		}
		if licErr.IsUnsupportedTag {
			// The server rejecting the tag *format* (e.g. a "dev" nightly or a custom
			// enterprise tag) is not a verdict on the license itself. Degrade like the
			// transport failure above: skip the pre-flight and let the container
			// validate the license at startup. The suggestion keeps a
			// typo'd tag diagnosable, since the later pull failure won't mention tags.
			opts.Logger.Info("license server does not support tag %q, deferring license validation to the emulator: %s", version, licErr.Detail)
			sink.Emit(output.MessageEvent{Severity: output.SeverityWarning, Text: fmt.Sprintf(
				"The license server does not support tag %q; the emulator will validate the license once it starts. If this is unintended, %s",
				version, config.TagSuggestion(),
			)})
			return false, nil
		}
		if !isDefinitiveLicenseRejection(licErr.Status) {
			// A 5xx outage, a 407 from a corporate proxy, or any other unexpected
			// status is not a verdict on the license. Degrade like the transport
			// failure above and let the container validate the license at startup.
			opts.Logger.Info("license server returned HTTP %d, deferring license validation to the emulator: %s", licErr.Status, licErr.Detail)
			sink.Emit(output.MessageEvent{Severity: output.SeverityWarning, Text: fmt.Sprintf(
				"The license server returned an unexpected response (HTTP %d); the emulator will validate the license once it starts", licErr.Status,
			)})
			return false, nil
		}
		if licErr.Detail != "" {
			opts.Logger.Error("license server response (HTTP %d): %s", licErr.Status, licErr.Detail)
		}
		// A definitive rejection also invalidates the cached license: drop it so a
		// later start (whose pre-flight may be skipped, e.g. when the image is
		// already local) cannot keep failing against the stale copy (DEVX-658).
		if rmErr := os.Remove(licenseFilePath); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			opts.Logger.Error("failed to remove cached license file: %v", rmErr)
		}
		opts.Telemetry.EmitEmulatorLifecycleEvent(ctx, telemetry.LifecycleEvent{
			EventType: telemetry.LifecycleStartError,
			Emulator:  containerConfig.EmulatorType,
			Image:     containerConfig.Image,
			ErrorCode: telemetry.ErrCodeLicenseInvalid,
			ErrorMsg:  err.Error(),
		})
		return false, &licenseRejectedError{productName: containerConfig.ProductName, version: version, licErr: licErr}
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
		} else {
			return true, nil
		}
	}

	return false, nil
}

// isDefinitiveLicenseRejection reports whether an HTTP status from the license
// server is a verdict on the token/license itself. Anything else (a 5xx outage,
// a 407 from a corporate proxy, ...) is not, and degrades to container-side
// validation instead of blocking the start.
func isDefinitiveLicenseRejection(status int) bool {
	return status == http.StatusBadRequest || status == http.StatusUnauthorized || status == http.StatusForbidden
}

// promptRelogin asks the user whether to run a fresh login after a definitive
// license rejection. Only call in interactive mode: the plain sink never
// answers input requests, so waiting on one would hang. The rejection reason is
// folded into the prompt itself (rather than emitted as a separate message
// first) so a decline doesn't show "License validation failed" twice — once
// here and again in the final ErrorEvent if the user says no.
func promptRelogin(ctx context.Context, sink output.Sink, licErr *api.LicenseError) bool {
	responseCh := make(chan output.InputResponse, 1)
	sink.Emit(output.UserInputRequestEvent{
		Prompt:     fmt.Sprintf("License validation failed: %s. Log in again to refresh your credentials?", licErr.Message),
		Options:    []output.InputOption{{Key: "enter", Label: "Press ENTER to log in again"}},
		ResponseCh: responseCh,
	})
	select {
	case resp := <-responseCh:
		return !resp.Cancelled
	case <-ctx.Done():
		return false
	}
}

const licenseMountPath = "/etc/localstack/conf.d/license.json"

// mountCachedLicense mounts the cached license file read-only into each
// container when it exists on disk, and reports whether it did.
func mountCachedLicense(containers []runtime.ContainerConfig, licenseFilePath string) bool {
	if _, err := os.Stat(licenseFilePath); err != nil {
		return false
	}
	for i := range containers {
		containers[i].Binds = append(containers[i].Binds, runtime.BindMount{
			HostPath:      licenseFilePath,
			ContainerPath: licenseMountPath,
			ReadOnly:      true,
		})
	}
	return true
}

func stripLicenseMount(containers []runtime.ContainerConfig) {
	for i := range containers {
		containers[i].Binds = slices.DeleteFunc(containers[i].Binds, func(b runtime.BindMount) bool {
			return b.ContainerPath == licenseMountPath
		})
	}
}

// licenseNotCoveredError is returned by startupMonitor.handleFailure when the container exits
// because the license does not include the emulator (Snowflake or Azure).
type licenseNotCoveredError struct{}

func (e *licenseNotCoveredError) Error() string {
	return "license does not include this emulator"
}

// licenseStartupError is returned by startContainers instead of rendering the
// failure when the container exits with license-related output while a retry
// with a refreshed license is possible — e.g. after validating a stale cached
// license.json mounted from an earlier run (DEVX-658). startWithLicenseRetry
// retries the start once with a freshly fetched license.
type licenseStartupError struct {
	name string
	logs string
}

func (e *licenseStartupError) Error() string {
	return fmt.Sprintf("%s exited during license validation:\n%s", e.name, e.logs)
}

// logsIndicateLicenseFailure reports whether a failed container's logs point at
// license validation rather than some other startup crash. Matching is loose on
// purpose: the emulator wording varies across products and versions, and a
// false positive only costs one extra license fetch and start attempt.
func logsIndicateLicenseFailure(logs string) bool {
	l := strings.ToLower(logs)
	if !strings.Contains(l, "license") {
		return false
	}
	for _, marker := range []string{"fail", "invalid", "expired", "error", "could not", "unable"} {
		if strings.Contains(l, marker) {
			return true
		}
	}
	return false
}

// containerExitedError is returned by startupMonitor.await when the container stops
// running before becoming healthy (e.g. it crashed during startup).
type containerExitedError struct {
	exitCode int // -1 when unknown
}

func (e *containerExitedError) Error() string {
	if e.exitCode < 0 {
		return "LocalStack exited unexpectedly"
	}
	return fmt.Sprintf("LocalStack exited unexpectedly (exit code %d)", e.exitCode)
}

// exitedError builds the containerExitedError for an exit detected by the
// IsRunning poll, waiting briefly for the runtime's exit wait to deliver the
// exit code (the wait response is already in flight once the container is seen
// stopped). Falls back to an unknown code (-1).
func exitedError(exitCh <-chan runtime.ExitResult) error {
	if exitCh != nil {
		select {
		case res := <-exitCh:
			if res.Err == nil {
				return &containerExitedError{exitCode: res.ExitCode}
			}
		case <-time.After(2 * time.Second):
		}
	}
	return &containerExitedError{exitCode: -1}
}

// startupTimeoutError is returned by startupMonitor.await when the container stays
// running but does not become healthy within the deadline. stopped records
// whether the container was stopped on the way out (the user chose "stop" at
// the interactive prompt) so the error messaging can reflect its actual state.
type startupTimeoutError struct {
	timeout time.Duration
	stopped bool
}

func (e *startupTimeoutError) Error() string {
	return fmt.Sprintf("LocalStack did not become ready within %s", e.timeout)
}

// maxStartupLogBytes bounds how much of a failing container's log tail is buffered
// to explain a crash during startup.
const maxStartupLogBytes = 64 * 1024

// The startup deadline plays a different role per mode: interactively it only
// shows a recoverable "keep waiting?" prompt, so it fires early; non-
// interactively it fails the start, so it stays conservative. LocalStack should
// not take this long either way, so a slower start is itself unusual and worth
// surfacing (LSTK_STARTUP_TIMEOUT overrides both).
const (
	defaultStartupTimeoutInteractive    = 20 * time.Second
	defaultStartupTimeoutNonInteractive = 60 * time.Second
)

// resolveStartupTimeout applies the per-mode default when no explicit timeout
// is configured.
func resolveStartupTimeout(timeout time.Duration, interactive bool) time.Duration {
	if timeout > 0 {
		return timeout
	}
	if interactive {
		return defaultStartupTimeoutInteractive
	}
	return defaultStartupTimeoutNonInteractive
}

// startupMonitor watches a just-started container until it becomes healthy,
// exits, or times out, and classifies/renders the failure when it doesn't. It
// bundles the collaborators and mode that stay constant across a Start
// invocation; per-container data flows through the method arguments.
type startupMonitor struct {
	rt          runtime.Runtime
	sink        output.Sink
	tel         *telemetry.Client
	timeout     time.Duration
	interactive bool
}

func newStartupMonitor(rt runtime.Runtime, sink output.Sink, tel *telemetry.Client, timeout time.Duration, interactive bool) *startupMonitor {
	return &startupMonitor{
		rt:          rt,
		sink:        sink,
		tel:         tel,
		timeout:     resolveStartupTimeout(timeout, interactive),
		interactive: interactive,
	}
}

// await polls until one of these outcomes:
//   - Success: health endpoint returns 200 (LocalStack is ready) → nil.
//   - Exit: the container stops running before becoming healthy →
//     *containerExitedError (exit code from exitCh when available, else -1).
//   - Timeout: the container stays running but is not healthy within the
//     deadline → *startupTimeoutError. In interactive mode the user is prompted
//     first (keep waiting re-arms the deadline; stop stops the container).
//
// exitCh delivers the container's exit as observed by the runtime. It may be nil
// if no wait could be registered; the IsRunning poll then still detects an exit
// (with an unknown exit code).
func (m *startupMonitor) await(ctx context.Context, containerID, healthURL string, exitCh <-chan runtime.ExitResult) error {
	client := &http.Client{Timeout: 2 * time.Second}

	deadline := time.NewTimer(m.timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	check := func() (ready bool, err error) {
		running, err := m.rt.IsRunning(ctx, containerID)
		if err != nil {
			// A canceled context (Ctrl+C during the readiness wait) surfaces here
			// as an IsRunning failure. Report it as the deliberate abort it is, not
			// a status-check failure, so Start classifies it as a cancellation and
			// the message stays stable. (Go 1.26's signal.NotifyContext attaches a
			// cause, so the wrapped Docker error would otherwise read
			// "...: interrupt signal received" instead of "context canceled".)
			if ctxErr := ctx.Err(); ctxErr != nil {
				return false, ctxErr
			}
			return false, fmt.Errorf("failed to check container status: %w", err)
		}
		if !running {
			// The poll can observe the exit before the runtime's wait response
			// arrives; give exitCh a moment to deliver the exit code rather than
			// reporting it unknown.
			return false, exitedError(exitCh)
		}

		resp, gerr := client.Get(healthURL)
		if gerr == nil && resp.StatusCode == http.StatusOK {
			if cerr := resp.Body.Close(); cerr != nil {
				m.sink.Emit(output.MessageEvent{Severity: output.SeverityWarning, Text: fmt.Sprintf("failed to close response body: %v", cerr)})
			}
			return true, nil
		}
		if resp != nil {
			if cerr := resp.Body.Close(); cerr != nil {
				m.sink.Emit(output.MessageEvent{Severity: output.SeverityWarning, Text: fmt.Sprintf("failed to close response body: %v", cerr)})
			}
		}
		return false, nil
	}

	// Probe once before the first tick so a fast start is caught promptly.
	if ready, err := check(); err != nil || ready {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case res := <-exitCh:
			if res.Err != nil {
				// The wait itself failed (e.g. an exit+removal race before the
				// wait registered). Stop trusting exitCh and let the IsRunning
				// poll detect the exit with an unknown code.
				exitCh = nil
				continue
			}
			return &containerExitedError{exitCode: res.ExitCode}
		case <-deadline.C:
			surface, stopped, err := m.handleTimeout(ctx, containerID)
			if err != nil {
				return err
			}
			if surface {
				return &startupTimeoutError{timeout: m.timeout, stopped: stopped}
			}
			// Keep waiting: re-arm the deadline and restore the spinner.
			deadline.Reset(m.timeout)
			m.sink.Emit(output.SpinnerStart("Starting LocalStack"))
		case <-ticker.C:
			if ready, err := check(); err != nil || ready {
				return err
			}
		}
	}
}

// handleTimeout decides what to do when the startup deadline elapses. In
// non-interactive mode it always surfaces the timeout, leaving the container
// running so the user can inspect it. In interactive mode it prompts the user to
// keep waiting or stop; "keep waiting" returns surface=false so the caller
// re-arms the deadline. stopped reports whether the container was stopped (the
// user chose "stop"), so the timeout error can describe its actual state.
func (m *startupMonitor) handleTimeout(ctx context.Context, containerID string) (surface, stopped bool, err error) {
	if !m.interactive {
		return true, false, nil
	}

	m.sink.Emit(output.SpinnerStop())
	responseCh := make(chan output.InputResponse, 1)
	m.sink.Emit(output.UserInputRequestEvent{
		Prompt: "LocalStack is taking longer than expected to start. Check logs with 'lstk logs'",
		Options: []output.InputOption{
			{Key: "w", Label: "Keep waiting [W]"},
			{Key: "s", Label: "Stop LocalStack and exit [S]"},
		},
		ResponseCh: responseCh,
	})

	select {
	case resp := <-responseCh:
		if resp.Cancelled {
			// Ctrl+C: leave the container running (it is detached).
			if ctx.Err() != nil {
				return false, false, ctx.Err()
			}
			return false, false, context.Canceled
		}
		if resp.SelectedKey == "s" {
			// Stopping takes a few seconds; show progress so the CLI does not
			// look hung after the keypress. The caller's SpinnerStop closes it.
			m.sink.Emit(output.SpinnerStart("Stopping LocalStack..."))
			// Best-effort stop; the timeout error is authoritative either way.
			_ = m.rt.Stop(ctx, containerID)
			return true, true, nil
		}
		return false, false, nil
	case <-ctx.Done():
		return false, false, ctx.Err()
	}
}

// lastLogLines returns the last n non-empty lines of logs, for including in an
// error summary. Returns "" when logs is empty.
func lastLogLines(logs string, n int) string {
	logs = strings.TrimRight(logs, "\n")
	if logs == "" {
		return ""
	}
	lines := strings.Split(logs, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// droppedHostEnv is a host variable filterHostEnv refused to forward, so the
// caller can warn the user. overrides names the critical variable the
// entrypoint-stripped name would clobber inside the emulator; it is empty when
// the entry was dropped for carrying a multi-line value instead.
type droppedHostEnv struct {
	name      string
	overrides string
}

// filterHostEnv returns the subset of host environment entries that should be
// forwarded to the emulator container. It keeps CI and LOCALSTACK_* variables
// but drops entries that would corrupt the emulator environment:
//   - LOCALSTACK_AUTH_TOKEN, so the host value cannot overwrite the token
//     resolved by lstk (which may come from the keyring); this drop is silent
//     because lstk forwards its own resolved token instead;
//   - values containing a newline or carriage return: the entrypoint re-exports
//     variables through a line-oriented `env | sed` pipeline, so an embedded
//     line like "LOCALSTACK_PATH=" would inject a rogue export that blanks PATH;
//   - variables whose prefix-stripped name would clobber a critical variable
//     inside the emulator (see criticalContainerVar).
//
// The latter two are returned in dropped so callers can warn the user.
func filterHostEnv(envList []string) (out []string, dropped []droppedHostEnv) {
	for _, e := range envList {
		key, value, ok := strings.Cut(e, "=")
		if !ok || (key != "CI" && !strings.HasPrefix(key, "LOCALSTACK_")) {
			continue
		}
		if key == "LOCALSTACK_AUTH_TOKEN" {
			continue
		}
		if strings.ContainsAny(value, "\n\r") {
			dropped = append(dropped, droppedHostEnv{name: key})
			continue
		}
		if name := strings.TrimPrefix(key, "LOCALSTACK_"); name != key && criticalContainerVar(name) {
			dropped = append(dropped, droppedHostEnv{name: key, overrides: name})
			continue
		}
		out = append(out, e)
	}
	return out, dropped
}

// criticalContainerVar reports whether a LOCALSTACK_* variable stripped to name
// would break the emulator: the image's docker-entrypoint.sh strips the
// LOCALSTACK_ prefix and re-exports the remainder (skipping only LOCALSTACK_HOST*
// and names starting with a digit), so e.g. a host LOCALSTACK_PATH becomes PATH
// inside the emulator and startup dies with "mkdir: command not found" (DEVX-984).
// Each entry has a verified failure mode in the image (localstack/lstk#378):
// HOME redirects every ~-anchored path (plugin cache, ~/.localstack, ~/.aws);
// IFS corrupts word splitting in the shell sourcing the re-exports; BASH_ENV
// names a file every non-interactive bash executes (e.g. init hooks); LD_*
// hijack the dynamic loader for every process; PYTHONHOME is fatal to the
// interpreter and PYTHONPATH shadows the emulator's imports.
func criticalContainerVar(name string) bool {
	switch name {
	case "PATH", "HOME", "IFS", "BASH_ENV",
		"LD_PRELOAD", "LD_LIBRARY_PATH", "PYTHONPATH", "PYTHONHOME":
		return true
	}
	return false
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

// envValue returns the value of the last entry matching key (KEY=value), or "".
func envValue(env []string, key string) string {
	prefix := key + "="
	value := ""
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			value = strings.TrimPrefix(e, prefix)
		}
	}
	return value
}

func agentEnv(cl caller.Classification) []string {
	var env []string
	if cl.AgentIdentity != "" {
		env = append(env, "AI_AGENT="+cl.AgentIdentity)
	}
	env = append(env, "LOCALSTACK_CLIENT_NAME=lstk", "LOCALSTACK_CLIENT_VERSION="+version.Version())
	return env
}

// checkSingleContainer rejects configs that enable more than one [[containers]]
// block. Only one emulator is supported at a time; running several together
// (e.g. AWS and Snowflake) is not supported yet and would collide on container
// names and shared ports during startup.
func checkSingleContainer(containers []config.ContainerConfig) error {
	if len(containers) > 1 {
		return fmt.Errorf("found %d [[containers]] blocks in your config, but only one is supported at a time", len(containers))
	}
	return nil
}

// servicePortRange returns the external service ports LocalStack opens for
// per-service access (4510-4559). The gateway ports (4566, 443, ...) are
// published separately from GATEWAY_LISTEN.
const servicePortRangeStart = 4510
const servicePortRangeEnd = 4559

func servicePortRange() []runtime.PortMapping {
	ports := make([]runtime.PortMapping, 0, servicePortRangeEnd-servicePortRangeStart+1)
	for p := servicePortRangeStart; p <= servicePortRangeEnd; p++ {
		ps := strconv.Itoa(p)
		ports = append(ports, runtime.PortMapping{ContainerPort: ps, HostPort: ps})
	}
	return ports
}
