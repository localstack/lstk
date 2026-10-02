package container

import (
	"context"
	"strings"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/runtime"
)

// bundledLicenseTokenEnv is baked into LocalStack Enterprise (offline) images
// next to their license file. The image's entrypoint uses it in place of any
// LOCALSTACK_AUTH_TOKEN, so the token lstk would inject is never used.
const bundledLicenseTokenEnv = "LOCALSTACK_AUTH_TOKEN_OVERRIDE"

// hasBundledLicense reports whether image is present locally and carries its
// own license (an offline image). Such an image needs nothing from the network:
// no pull, no auth token, and no license pre-flight. It is detected from the
// local image config only, so a missing image or an inspect error means false.
// Any image baking in this variable qualifies, e.g. a custom build with a CI
// token; the emulator still validates whatever license it ends up with.
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

// NeedsAuthToken reports whether starting opts.Containers requires an auth
// token. It is false only on positive evidence that lstk makes no
// authenticated call and the container brings its own license: every image
// is a locally present image with a bundled license. Anything else, including
// a failed image inspect, keeps the token (and the interactive login) required.
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
