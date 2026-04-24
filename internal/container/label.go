package container

import (
	"context"
	"os"
	stdruntime "runtime"
	"time"

	"github.com/localstack/lstk/internal/api"
	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/log"
)

const NoLicenseLabel = "LocalStack (No license)"

// ResolveEmulatorLabel tries to fetch the plan name from the license API
// to build a label like "LocalStack Ultimate". Falls back to
// NoLicenseLabel when the plan cannot be determined. The returned bool
// is true only when a real plan was resolved (i.e. the result is worth caching).
func ResolveEmulatorLabel(ctx context.Context, client api.PlatformAPI, containers []config.ContainerConfig, token string, logger log.Logger) (string, bool) {
	if len(containers) == 0 || token == "" {
		return NoLicenseLabel, false
	}

	c := containers[0]

	licenseProductName, err := c.LicenseProductName()
	if err != nil {
		return NoLicenseLabel, false
	}

	tag := c.Tag
	if tag == "" || tag == "latest" {
		if c.Type == config.EmulatorSnowflake {
			return "LocalStack Snowflake", false
		}
		apiCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		v, err := client.GetLatestCatalogVersion(apiCtx, string(c.Type))
		cancel()
		if err != nil {
			logger.Info("could not resolve catalog version for header: %v", err)
			return NoLicenseLabel, false
		}
		tag = v
	}

	hostname, _ := os.Hostname()
	licReq := &api.LicenseRequest{
		Product:     api.ProductInfo{Name: licenseProductName, Version: tag},
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
