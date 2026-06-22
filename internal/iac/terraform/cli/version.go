package cli

import (
	"context"
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"
)

// legacyEndpointVersion is the Terraform/OpenTofu version at which the S3
// backend gained the nested `endpoints {}` map and deprecated the flat
// `endpoint`/`dynamodb_endpoint`/`iam_endpoint`/`sts_endpoint` keys. Versions
// below this use the legacy flat form. OpenTofu forked from Terraform 1.6, so
// its minimum version is already modern and it never takes the legacy path.
var legacyEndpointVersion = [2]int{1, 6}

// tfVersionOutput is the subset of `<tfBin> version -json` we need. Both
// terraform and tofu emit a `terraform_version` field.
type tfVersionOutput struct {
	TerraformVersion string `json:"terraform_version"`
}

// usesLegacyEndpoints reports whether the in-use Terraform/OpenTofu emits the
// legacy flat S3-backend endpoint keys (version < 1.6) rather than the modern
// `endpoints {}` map. The version is probed via `<tfBin> version -json`; when it
// cannot be determined (probe fails, unparseable output, missing field) the
// function defaults to the modern map form (returns false), which is correct for
// the overwhelming majority of current installs.
func usesLegacyEndpoints(ctx context.Context, tfBin, workdir string) bool {
	cmd := exec.CommandContext(ctx, tfBin, "version", "-json")
	cmd.Dir = workdir
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return parseUsesLegacyEndpoints(out)
}

// parseUsesLegacyEndpoints returns true only when the parsed terraform_version
// is strictly below legacyEndpointVersion. Any parse failure defaults to the
// modern form (false).
func parseUsesLegacyEndpoints(versionJSON []byte) bool {
	var v tfVersionOutput
	if err := json.Unmarshal(versionJSON, &v); err != nil {
		return false
	}
	major, minor, ok := parseMajorMinor(v.TerraformVersion)
	if !ok {
		return false
	}
	if major != legacyEndpointVersion[0] {
		return major < legacyEndpointVersion[0]
	}
	return minor < legacyEndpointVersion[1]
}

// parseMajorMinor extracts the major and minor components from a version string
// like "1.6.0" or "1.5.7-dev". It returns ok=false if the leading two
// dot-separated components are not integers.
func parseMajorMinor(version string) (major, minor int, ok bool) {
	version = strings.TrimSpace(version)
	parts := strings.SplitN(version, ".", 3)
	if len(parts) < 2 {
		return 0, 0, false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, false
	}
	// Trim any pre-release/build suffix on the minor component (e.g. "6-dev").
	minorField := parts[1]
	if i := strings.IndexFunc(minorField, func(r rune) bool { return r < '0' || r > '9' }); i >= 0 {
		minorField = minorField[:i]
	}
	minor, err = strconv.Atoi(minorField)
	if err != nil {
		return 0, 0, false
	}
	return major, minor, true
}
