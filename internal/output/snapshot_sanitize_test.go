package output

import (
	"regexp"
	"testing"
)

// snap.MatchJSON masks volatile values via dotted paths, but plain-text
// snap.Match has no masking hook. sanitizeSnapshot fills that gap: it
// replaces volatile values in rendered CLI output with stable placeholders
// so text snapshots stay deterministic across runs.
//
// The patterns are label-anchored rather than value-shaped on purpose: Go's
// RE2 has no lookahead, so a bare semver pattern (\d+\.\d+\.\d+) would also
// match inside IPv4 addresses ("127.0.0.1" contains "0.0.1" at a word
// boundary). Anchoring on the rendered label sidesteps that class of
// collision; add a new labeled pattern here when another volatile value
// shows up in snapshot output.
var (
	snapshotVersionRe = regexp.MustCompile(`(Version: )\S+`)
	snapshotUptimeRe  = regexp.MustCompile(`(?m)(Uptime: ).+$`)
)

func sanitizeSnapshot(s string) string {
	s = snapshotVersionRe.ReplaceAllString(s, "${1}<version>")
	s = snapshotUptimeRe.ReplaceAllString(s, "${1}<time>")
	return s
}

func TestSanitizeSnapshot(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"• Version: 4.14.1":            "• Version: <version>",
		"• Version: v2.3.0-rc1":        "• Version: <version>",
		"• Uptime: 4m 23s":             "• Uptime: <time>",
		"• Uptime: 1h 2m 3s":           "• Uptime: <time>",
		"• Uptime: 42s":                "• Uptime: <time>",
		"• Endpoint: 127.0.0.1:4566":   "• Endpoint: 127.0.0.1:4566",
		"plain text untouched":         "plain text untouched",
		"• Version: 4.14.1\n• Uptime: 4m 23s\n• Container: localstack-aws": "• Version: <version>\n• Uptime: <time>\n• Container: localstack-aws",
	}
	for in, want := range cases {
		if got := sanitizeSnapshot(in); got != want {
			t.Errorf("sanitizeSnapshot(%q) = %q, want %q", in, got, want)
		}
	}
}
