package cli

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
)

// versionRe matches the leading MAJOR.MINOR.PATCH of a `cdk --version` line
// (e.g. "2.177.0 (build abc1234)").
var versionRe = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

// CheckVersion runs `<cdkBin> --version` and returns an error if the reported
// version is below the minimum lstk supports, or if the output cannot be
// parsed. lstk points CDK at LocalStack purely through environment variables,
// which only CDK >= minCDKVersionString honors; on an older (or unparseable)
// version lstk must refuse to run so it cannot silently target real AWS.
func CheckVersion(ctx context.Context, cdkBin string) error {
	out, err := exec.CommandContext(ctx, cdkBin, "--version").Output()
	if err != nil {
		return fmt.Errorf("could not determine cdk version (run `%s --version`): %w", cdkBin, err)
	}
	return checkVersionString(string(out))
}

func checkVersionString(out string) error {
	m := versionRe.FindStringSubmatch(out)
	if m == nil {
		return fmt.Errorf("could not parse cdk version from %q; lstk requires AWS CDK %s or newer", out, minCDKVersionString)
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	patch, _ := strconv.Atoi(m[3])
	if !atLeastMinVersion(major, minor, patch) {
		return fmt.Errorf("AWS CDK %d.%d.%d is too old; lstk requires %s or newer (it points CDK at LocalStack via AWS_ENDPOINT_URL, which older versions ignore)", major, minor, patch, minCDKVersionString)
	}
	return nil
}

func atLeastMinVersion(major, minor, patch int) bool {
	switch {
	case major != minCDKMajor:
		return major > minCDKMajor
	case minor != minCDKMinor:
		return minor > minCDKMinor
	default:
		return patch >= minCDKPatch
	}
}
