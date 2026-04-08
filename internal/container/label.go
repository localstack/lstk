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

// ResolveEmulatorLabel tries to fetch the plan name from the license API
// to build a label like "LocalStack Ultimate". Falls back to
// "LocalStack (No license)" when the plan cannot be determined.
func ResolveEmulatorLabel(ctx context.Context, client api.PlatformAPI, containers []config.ContainerConfig, token string, logger log.Logger) string {
	const noLicense = "LocalStack (No license)"

	if len(containers) == 0 || token == "" {
		return noLicense
	}

	c := containers[0]

	productName, err := c.ProductName()
	if err != nil {
		return noLicense
	}

	tag := c.Tag
	if tag == "" || tag == "latest" {
		apiCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		v, err := client.GetLatestCatalogVersion(apiCtx, string(c.Type))
		if err != nil {
			logger.Info("could not resolve catalog version for header: %v", err)
			return noLicense
		}
		tag = v
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
		return noLicense
	}

	if plan := resp.PlanDisplayName(); plan != "" {
		return "LocalStack " + plan
	}
	return noLicense
}
