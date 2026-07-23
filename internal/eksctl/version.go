package eksctl

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
)

// minEksctlVersion is the lowest eksctl version lstk supports. 0.181.0 is where
// eksctl moved to per-client resolution of the AWS_*_ENDPOINT variables lstk
// sets, and the boundary the LocalStack eksctl docs define for the env-var
// ("Newer Versions") flow. Older releases resolved the same variables through a
// deprecated SDK global-resolver path the docs route through `--profile
// localstack` instead; lstk supports only the documented env-var flow, so it
// refuses to run against them rather than risk requests escaping to real AWS.
const (
	minEksctlMajor = 0
	minEksctlMinor = 181
	minEksctlPatch = 0
)

// minEksctlVersionString is the human-facing form used in error messages.
const minEksctlVersionString = "0.181.0"

// CheckVersion runs `<bin> version` and returns an error if the reported version
// is below the minimum lstk supports, or if the output cannot be parsed. lstk
// points eksctl at LocalStack purely through environment variables — the flow
// LocalStack documents and tests from eksctl >= minEksctlVersionString; on an
// older (or unparseable) version lstk must refuse to run so requests cannot
// silently escape to real AWS.
func CheckVersion(ctx context.Context, bin string) error {
	out, err := exec.CommandContext(ctx, bin, "version").Output()
	if err != nil {
		return fmt.Errorf("could not determine eksctl version (run `%s version`): %w", bin, err)
	}
	return checkVersionString(string(out))
}

func checkVersionString(out string) error {
	versionRe := regexp.MustCompile(`^\s*v?(\d+)\.(\d+)\.(\d+)(-[^\s+]+)?(?:\+\S+)?\s*$`)
	m := versionRe.FindStringSubmatch(out)
	if m == nil {
		return fmt.Errorf("could not parse eksctl version from %q; lstk requires eksctl %s or newer", out, minEksctlVersionString)
	}
	major, majorErr := strconv.Atoi(m[1])
	minor, minorErr := strconv.Atoi(m[2])
	patch, patchErr := strconv.Atoi(m[3])
	if majorErr != nil || minorErr != nil || patchErr != nil {
		return fmt.Errorf("could not parse eksctl version from %q; lstk requires eksctl %s or newer", out, minEksctlVersionString)
	}
	if !atLeastMinVersion(major, minor, patch) || (major == minEksctlMajor && minor == minEksctlMinor && patch == minEksctlPatch && m[4] != "") {
		return fmt.Errorf("eksctl %d.%d.%d is too old; lstk requires %s or newer (the earliest version lstk supports pointing at LocalStack via the AWS_*_ENDPOINT variables)", major, minor, patch, minEksctlVersionString)
	}
	return nil
}

func atLeastMinVersion(major, minor, patch int) bool {
	switch {
	case major != minEksctlMajor:
		return major > minEksctlMajor
	case minor != minEksctlMinor:
		return minor > minEksctlMinor
	default:
		return patch >= minEksctlPatch
	}
}
