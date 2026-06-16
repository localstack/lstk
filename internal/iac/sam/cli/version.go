package cli

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
)

// versionRe matches the leading MAJOR.MINOR.PATCH of a `sam --version` line
// (e.g. "SAM CLI, version 1.151.0").
var versionRe = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

// CheckVersion runs `<samBin> --version` and returns an error if the reported
// version is below the minimum lstk supports, or if the output cannot be parsed.
// lstk points SAM at LocalStack purely through environment variables, which only
// SAM >= minSAMVersionString honors; on an older (or unparseable) version lstk
// must refuse to run so it cannot silently target real AWS.
func CheckVersion(ctx context.Context, samBin string) error {
	out, err := exec.CommandContext(ctx, samBin, "--version").Output()
	if err != nil {
		return fmt.Errorf("could not determine sam version (run `%s --version`): %w", samBin, err)
	}
	return checkVersionString(string(out))
}

func checkVersionString(out string) error {
	m := versionRe.FindStringSubmatch(out)
	if m == nil {
		return fmt.Errorf("could not parse sam version from %q; lstk requires AWS SAM CLI %s or newer", out, minSAMVersionString)
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	patch, _ := strconv.Atoi(m[3])
	if !atLeastMinVersion(major, minor, patch) {
		return fmt.Errorf("AWS SAM CLI %d.%d.%d is too old; lstk requires %s or newer (it points SAM at LocalStack via AWS_ENDPOINT_URL, which older versions ignore)", major, minor, patch, minSAMVersionString)
	}
	return nil
}

func atLeastMinVersion(major, minor, patch int) bool {
	switch {
	case major != minSAMMajor:
		return major > minSAMMajor
	case minor != minSAMMinor:
		return minor > minSAMMinor
	default:
		return patch >= minSAMPatch
	}
}
