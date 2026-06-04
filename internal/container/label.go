package container

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	stdruntime "runtime"
	"time"

	"github.com/localstack/lstk/internal/api"
	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/log"
)

// ResolveAndCacheLabel resolves the plan label using the version returned by Start
// and caches it for subsequent runs. resolvedVersion is the version extracted from
// image inspection; it may be empty if Start returned early (e.g. already running).
func ResolveAndCacheLabel(ctx context.Context, opts StartOptions, resolvedVersion string, labelCh chan<- string) {
	label, ok := ResolveEmulatorLabel(ctx, opts.PlatformClient, opts.Containers, opts.AuthToken, resolvedVersion, opts.Logger)
	if ok {
		config.CachePlanLabel(label)
	}
	labelCh <- label
}

const NoLicenseLabel = "LocalStack (No license)"

// ResolveEmulatorLabel tries to fetch the plan name from the license API
// to build a label like "LocalStack Ultimate". Falls back to
// NoLicenseLabel when the plan cannot be determined. The returned bool
// is true only when a real plan was resolved (i.e. the result is worth caching).
// resolvedVersion is the version from post-pull image inspection for "latest" containers.
func ResolveEmulatorLabel(ctx context.Context, client api.PlatformAPI, containers []config.ContainerConfig, token, resolvedVersion string, logger log.Logger) (string, bool) {
	if len(containers) == 0 || token == "" {
		return NoLicenseLabel, false
	}

	c := containers[0]

	// Self-validating emulators never hit the license API (it has no catalog
	// entry for their products); their plan comes from the license the
	// container activated itself.
	if c.Type.SelfValidatesLicense() {
		if label, ok := activatedLicenseLabel(c, logger); ok {
			return label, true
		}
		return "LocalStack", false
	}

	productName, err := c.ProductName()
	if err != nil {
		return NoLicenseLabel, false
	}

	tag := c.Tag
	if tag == "" || tag == "latest" {
		if resolvedVersion == "" {
			return NoLicenseLabel, false
		}
		tag = resolvedVersion
	}

	hostname, _ := os.Hostname()
	licReq := &api.LicenseRequest{
		Product:     api.ProductInfo{Name: productName, Version: tag},
		Credentials: api.CredentialsInfo{Token: token},
		Machine:     api.MachineInfo{Hostname: hostname, Platform: stdruntime.GOOS, PlatformRelease: stdruntime.GOARCH},
	}

	licCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	resp, err := client.GetLicense(licCtx, licReq)
	if err != nil {
		logger.Info("could not fetch license for header: %v", err)
		return NoLicenseLabel, false
	}

	if plan := resp.PlanDisplayName(); plan != "" {
		return "LocalStack " + plan, true
	}
	return NoLicenseLabel, false
}

// activatedLicenseLabel reads the license a self-validating emulator activated
// during startup and cached at /var/lib/localstack/cache/license.json — inside
// the volume lstk mounts, so it is readable from the host.
func activatedLicenseLabel(c config.ContainerConfig, logger log.Logger) (string, bool) {
	volumeDir, err := c.VolumeDir()
	if err != nil {
		return "", false
	}
	licensePath := filepath.Join(volumeDir, "cache", "license.json")
	data, err := os.ReadFile(licensePath)
	if err != nil {
		logger.Info("could not read activated license for header: %v", err)
		return "", false
	}
	var lic api.LicenseResponse
	if err := json.Unmarshal(data, &lic); err != nil {
		logger.Info("could not parse activated license %s: %v", licensePath, err)
		return "", false
	}
	if plan := lic.PlanDisplayName(); plan != "" {
		return "LocalStack " + plan, true
	}
	return "", false
}
