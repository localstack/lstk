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

// Version is a parsed `cdk --version`. It is returned by CheckVersion so
// callers can gate behavior on the CLI's capabilities without paying for a
// second subprocess.
type Version struct{ Major, Minor, Patch int }

// SupportsPathStyleFlag reports whether this CDK honors CDK_S3_FORCE_PATH_STYLE.
// Below the threshold the flag is ignored and CDK uses virtual-host addressing,
// which needs the `s3.`-prefixed S3 endpoint — the two travel together.
func (v Version) SupportsPathStyleFlag() bool {
	switch {
	case v.Major != pathStyleFlagMajor:
		return v.Major > pathStyleFlagMajor
	case v.Minor != pathStyleFlagMinor:
		return v.Minor > pathStyleFlagMinor
	default:
		return v.Patch >= pathStyleFlagPatch
	}
}

// CheckVersion runs `<cdkBin> --version` and returns the parsed version, or an
// error if the reported version is below the minimum lstk supports or the
// output cannot be parsed. lstk points CDK at LocalStack purely through
// environment variables, which only CDK >= minCDKVersionString honors; on an
// older (or unparseable) version lstk must refuse to run so it cannot silently
// target real AWS.
func CheckVersion(ctx context.Context, cdkBin string) (Version, error) {
	out, err := exec.CommandContext(ctx, cdkBin, "--version").Output()
	if err != nil {
		return Version{}, fmt.Errorf("could not determine cdk version (run `%s --version`): %w", cdkBin, err)
	}
	return checkVersionString(string(out))
}

func checkVersionString(out string) (Version, error) {
	m := versionRe.FindStringSubmatch(out)
	if m == nil {
		return Version{}, fmt.Errorf("could not parse cdk version from %q; lstk requires AWS CDK %s or newer", out, minCDKVersionString)
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	patch, _ := strconv.Atoi(m[3])
	if !atLeastMinVersion(major, minor, patch) {
		return Version{}, fmt.Errorf("AWS CDK %d.%d.%d is too old; lstk requires %s or newer (it points CDK at LocalStack via AWS_ENDPOINT_URL, which older versions ignore)", major, minor, patch, minCDKVersionString)
	}
	return Version{Major: major, Minor: minor, Patch: patch}, nil
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
