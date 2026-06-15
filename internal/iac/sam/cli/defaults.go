package cli

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
// offline subcommands that need no running emulator.
func IsOffline(args []string) bool {
	return offlineCommands[subcommand(args)]
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
