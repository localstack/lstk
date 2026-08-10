package cli

import "strings"

// minSAMVersion is the lowest AWS SAM CLI version lstk supports. From this
// version SAM honors AWS_ENDPOINT_URL (via its bundled botocore), which is how
// lstk points SAM at LocalStack. Older versions ignore it and would silently
// target real AWS, so lstk refuses to run against them. See the sam-proxy design
// doc for the full rationale.
const (
	minSAMMajor = 1
	minSAMMinor = 95
	minSAMPatch = 0
)

// minSAMVersionString is the human-facing form used in error messages.
const minSAMVersionString = "1.95.0"

// offlineCommands are the SAM subcommands that never contact AWS APIs and so do
// not require a running emulator. Everything else (deploy, sync, package,
// delete, logs, traces, list, remote, publish, …) is treated as AWS-contacting
// and is gated on a running AWS emulator.
var offlineCommands = map[string]bool{
	"docs":     true,
	"init":     true,
	"build":    true,
	"validate": true,
	"local":    true,
	"pipeline": true,
}

// valueFlags are SAM global options that consume the following token as their
// value, so the subcommand scan must skip both the flag and its value. SAM's
// global options before the subcommand (`--debug`, `--beta-features`, `--info`,
// `--version`, `-h`) are all boolean, so this is currently empty; it is kept for
// structural parity with the cdk proxy and to make adding one a one-line change.
var valueFlags = map[string]bool{}

// IsOffline reports whether the SAM invocation described by args is one of the
// offline subcommands that need no running emulator, or a help request.
func IsOffline(args []string) bool {
	return IsHelp(args) || offlineCommands[subcommand(args)]
}

// withRegionFlag returns args with `--region <region>` appended, so the region
// reaches SAM as a command-line option rather than only as AWS_REGION in the
// environment.
//
// This is not redundant with BuildEnv. SAM injects samconfig.toml values as if
// they had been typed on the command line, so a `region` key there outranks
// every environment variable — measured: with samconfig.toml naming us-east-1
// and AWS_REGION/AWS_DEFAULT_REGION both naming ap-northeast-1, SAM signs for
// us-east-1. Only an actual command-line --region beats it. (The account has no
// samconfig.toml equivalent, which is why it never suffered the same defeat.)
//
// Two guards. Offline subcommands are skipped because `init` and `docs` reject
// --region outright; every AWS-contacting subcommand accepts it. And an existing
// --region in args is left alone: it is the user addressing sam directly, and
// appending ours after theirs would silently outrank it.
func withRegionFlag(args []string, region string) []string {
	if region == "" || IsOffline(args) || hasRegionFlag(args) {
		return args
	}
	out := make([]string, 0, len(args)+2)
	out = append(out, args...)
	return append(out, "--region", region)
}

func hasRegionFlag(args []string) bool {
	for _, a := range args {
		if a == "--region" || strings.HasPrefix(a, "--region=") {
			return true
		}
	}
	return false
}

// helpFlags are the flags sam recognizes as a help request.
var helpFlags = map[string]bool{"-h": true, "--help": true}

// IsHelp reports whether args requests sam's help output. sam answers this
// without needing a running emulator, same as the other offline commands.
func IsHelp(args []string) bool {
	for _, a := range args {
		if helpFlags[a] {
			return true
		}
	}
	return false
}

// subcommand returns the first non-flag token in args that is not consumed as a
// global option's value, or "" if there is none.
func subcommand(args []string) string {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if len(a) == 0 {
			continue
		}
		if a[0] == '-' {
			if valueFlags[a] && i+1 < len(args) {
				i++ // skip this flag's value
			}
			continue
		}
		return a
	}
	return ""
}
