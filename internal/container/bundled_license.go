package container

import (
	"context"
	"strings"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/runtime"
)

// bundledLicenseTokenEnv is set by Enterprise offline images; their entrypoint
// prefers it over LOCALSTACK_AUTH_TOKEN.
const bundledLicenseTokenEnv = "LOCALSTACK_AUTH_TOKEN_OVERRIDE"

// hasBundledLicense reports whether image is local and bundles its own license.
// Any inspect error counts as false, so detection fails closed.
func hasBundledLicense(ctx context.Context, rt runtime.Runtime, image string) bool {
	env, err := rt.ImageEnv(ctx, image)
	if err != nil {
		return false
	}
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, bundledLicenseTokenEnv+"="); ok && strings.TrimSpace(v) != "" {
			return true
		}
	}
	return false
}

// NeedsAuthToken reports whether starting opts.Containers needs an auth token:
// true unless every image is a local bundled-license image.
func NeedsAuthToken(ctx context.Context, rt runtime.Runtime, opts StartOptions) bool {
	if len(opts.Containers) == 0 {
		return true
	}
	for _, c := range opts.Containers {
		image, err := resolveImage(c, opts.ImageOverride)
		if err != nil || !hasBundledLicense(ctx, rt, image) {
			return true
		}
	}
	return false
}

// resolveImage returns the image c starts from, with the --image override applied.
func resolveImage(c config.ContainerConfig, imageOverride string) (string, error) {
	if imageOverride != "" {
		c.CustomImage = imageOverride
	}
	return c.Image()
}
