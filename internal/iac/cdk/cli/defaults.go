package cli

// minCDKVersion is the lowest AWS CDK CLI version lstk supports. From this
// version the CDK CLI honors AWS_ENDPOINT_URL / AWS_ENDPOINT_URL_S3, which is
// how lstk points CDK at LocalStack. Older versions ignore those variables and
// would silently target real AWS, so lstk refuses to run against them. See the
// cdk-proxy design doc for the full rationale.
const (
	minCDKMajor = 2
	minCDKMinor = 177
	minCDKPatch = 0
)

// minCDKVersionString is the human-facing form used in error messages.
const minCDKVersionString = "2.177.0"

// offlineCommands are the CDK subcommands that never contact AWS APIs and so do
// not require a running emulator. Everything else (bootstrap, deploy, destroy,
// diff, import, watch, rollback, gc, migrate, …) is treated as AWS-contacting
// and is gated on a running AWS emulator. The set is fixed and intentionally
// not configurable, mirroring the terraform proxy's unproxied-command set.
//
// The LocalStack-pointed environment is still applied to offline commands (it
// is harmless when no API call is made), so a `synth` that performs a context
// lookup still routes to LocalStack; only the emulator-running gate differs.
var offlineCommands = map[string]bool{
	"init":        true,
	"synth":       true,
	"synthesize":  true,
	"ls":          true,
	"list":        true,
	"version":     true,
	"doctor":      true,
	"acknowledge": true,
	"ack":         true,
	"context":     true,
	"notices":     true,
}

// valueFlags are CDK global options that consume the following token as their
// value, so the subcommand scan must skip both the flag and its value. Only the
// space-separated form needs listing; the --flag=value form is a single token
// and is skipped as an ordinary flag. This covers the common value-taking
// global options; an unlisted one could cause its value to be mistaken for the
// subcommand, which at worst gates an offline command on a running emulator
// (the safe direction — never a wrong real-AWS call).
var valueFlags = map[string]bool{
	"--app": true, "-a": true,
	"--context": true, "-c": true,
	"--output": true, "-o": true,
	"--profile":  true,
	"--role-arn": true, "-r": true,
	"--plugin":              true,
	"--proxy":               true,
	"--ca-bundle-path":      true,
	"--toolkit-stack-name":  true,
	"--toolkit-bucket-name": true,
}

// IsOffline reports whether the CDK invocation described by args is one of the
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
