package integration_test

import (
	"regexp"
	"strings"
)

// snap.Match has no masking hook (snap.MatchJSON's dotted paths are JSON
// only), so CLI output is passed through sanitizeOutput before snapshotting.
// Patterns are anchored on stable context (label, host, filename prefix)
// rather than value-shaped: Go's RE2 has no lookahead, and a bare
// number/semver pattern would corrupt neighbouring text such as IPv4
// addresses. Add a rule here when a new volatile value shows up in
// snapshot output.
var (
	// httptest servers listen on a random localhost port.
	sanitizePortRe = regexp.MustCompile(`(https?://127\.0\.0\.1):\d+`)
	// Release asset names embed the host platform (lstk_0.0.2_darwin_arm64.tar.gz).
	sanitizeAssetRe = regexp.MustCompile(`\blstk_([^_\s]+)_[a-z0-9]+_[a-z0-9]+(\.tar\.gz|\.zip)`)
	// Rendered instance info lines with per-run values.
	sanitizeVersionRe = regexp.MustCompile(`(Version: )\S+`)
	sanitizeUptimeRe  = regexp.MustCompile(`(?m)(Uptime: ).+$`)
	// The emulator endpoint host depends on whether localhost.localstack.cloud
	// resolves in the environment; scheme and port stay pinned.
	sanitizeEndpointHostRe = regexp.MustCompile(`(ENV_AWS_ENDPOINT_URL(?:_S3)?=https?://)[^:\s]+`)
	// Any URL pointing at the default gateway port (terraform override
	// endpoints, rendered endpoint lines) — anchored on :4566 so it can't
	// touch unrelated URLs.
	sanitizeGatewayHostRe = regexp.MustCompile(`(https?://)[^:"\s]+(:4566)`)
	// Rendered snapshot metadata: timestamps ("2026-04-15 14:32 UTC"),
	// human-readable sizes ("47.3 MB"), and LocalStack calendar versions
	// ("2026.03"). Fixture-driven today, but masked so fixture tweaks don't
	// churn snapshots.
	sanitizeDateRe   = regexp.MustCompile(`\d{4}-\d{2}-\d{2} \d{2}:\d{2} UTC`)
	sanitizeSizeRe   = regexp.MustCompile(`\b\d+(?:\.\d+)? (?:B|KB|MB|GB)\b`)
	sanitizeCalverRe = regexp.MustCompile(`\b20\d{2}\.\d{2}\b`)
	// Reference-extension echo lines carrying temp paths or random ids.
	sanitizeExtPathRe = regexp.MustCompile(`(?m)^(SELF=|CONFIG_DIR=).*$`)
	sanitizeExtIDRe   = regexp.MustCompile(`(?m)^(SESSION_ID=).*$`)
)

func sanitizeOutput(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = sanitizePortRe.ReplaceAllString(s, "${1}:<port>")
	s = sanitizeAssetRe.ReplaceAllString(s, "lstk_${1}_<platform>${2}")
	s = sanitizeVersionRe.ReplaceAllString(s, "${1}<version>")
	s = sanitizeUptimeRe.ReplaceAllString(s, "${1}<time>")
	s = sanitizeEndpointHostRe.ReplaceAllString(s, "${1}<host>")
	s = sanitizeGatewayHostRe.ReplaceAllString(s, "${1}<endpoint-host>${2}")
	s = sanitizeExtPathRe.ReplaceAllString(s, "${1}<path>")
	s = sanitizeExtIDRe.ReplaceAllString(s, "${1}<id>")
	s = sanitizeDateRe.ReplaceAllString(s, "<date>")
	s = sanitizeSizeRe.ReplaceAllString(s, "<size>")
	s = sanitizeCalverRe.ReplaceAllString(s, "<calver>")
	// Wrapped-tool env dumps echo GOOS-dependent endpoint hosts.
	s = strings.ReplaceAll(s, "host.docker.internal", "<endpoint-host>")
	s = strings.ReplaceAll(s, "localhost.localstack.cloud", "<endpoint-host>")
	return s
}
