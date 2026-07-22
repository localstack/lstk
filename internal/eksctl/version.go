package eksctl

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
)

// minEksctlVersion is the lowest eksctl version lstk supports. From this version
// (the "Newer Versions" flow in the LocalStack eksctl docs) eksctl routes AWS
// services through the AWS_*_ENDPOINT environment variables lstk sets. Older
// releases ignore them and would silently target real AWS, so lstk refuses to
// run against them.
const (
	minEksctlMajor = 0
	minEksctlMinor = 181
	minEksctlPatch = 0
)

// minEksctlVersionString is the human-facing form used in error messages.
const minEksctlVersionString = "0.181.0"

// versionRe matches the leading MAJOR.MINOR.PATCH of `eksctl version` output
// (e.g. "0.211.0").
var versionRe = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

// CheckVersion runs `<bin> version` and returns an error if the reported version
// is below the minimum lstk supports, or if the output cannot be parsed. lstk
// points eksctl at LocalStack purely through environment variables, which only
// eksctl >= minEksctlVersionString honors; on an older (or unparseable) version
// lstk must refuse to run so it cannot silently target real AWS.
func CheckVersion(ctx context.Context, bin string) error {
	out, err := exec.CommandContext(ctx, bin, "version").Output()
	if err != nil {
		return fmt.Errorf("could not determine eksctl version (run `%s version`): %w", bin, err)
	}
	return checkVersionString(string(out))
}

func checkVersionString(out string) error {
	m := versionRe.FindStringSubmatch(out)
	if m == nil {
		return fmt.Errorf("could not parse eksctl version from %q; lstk requires eksctl %s or newer", out, minEksctlVersionString)
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	patch, _ := strconv.Atoi(m[3])
	if !atLeastMinVersion(major, minor, patch) {
		return fmt.Errorf("eksctl %d.%d.%d is too old; lstk requires %s or newer (it points eksctl at LocalStack via the AWS_*_ENDPOINT variables, which older versions ignore)", major, minor, patch, minEksctlVersionString)
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
